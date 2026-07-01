-- Fixture battery for the pgexplain byte-fidelity guard
-- (pkg/pgexplain/fidelity_test.go, TestFidelityAgainstLiveServer).
--
-- Each record is separated by a blank line and is parsed by the Go test, not run
-- through psql directly. Recognised comment directives:
--   -- name:  <slug>                 (required) sub-test name
--   -- setup: <stmt>; <stmt>; ...    (optional) SET LOCAL statements run in the
--                                    same transaction before both EXPLAIN captures
-- All other `--` lines are human-readable notes. The single non-comment line is
-- the query (its trailing `;` is stripped). The schema these run against is seeded
-- from schema.sql. Every query targets a specific fidelity fix or parity control;
-- the guard renders EXPLAIN (... FORMAT JSON) through joe and byte-compares the
-- normalized plan body against EXPLAIN (... FORMAT TEXT) from the same server.

-- name: seq_scan_filter_removed_nonzero
-- FIX-1 (>0 control): the Seq Scan Filter removes 5 rows (val 1..5), so psql AND
-- joe both print "Rows Removed by Filter: 5". Reverting FIX-1 keeps this green,
-- which is what makes it the control for the zero-removed cases below.
select * from items where val > 5;

-- name: having_zero_removed
-- FIX-1 (=0): every group has 200 rows, so HAVING count(*)>100 removes 0 groups;
-- psql suppresses "Rows Removed by Filter: 0" and post-FIX-1 joe must too.
select cat, count(*) from items group by cat having count(*) > 100;

-- name: cte_scan_filter_zero_removed
-- FIX-1 (=0) on a CTE Scan: c>0 removes nothing from the materialized CTE.
with m as materialized (select cat, count(*) c from items group by cat) select * from m where c > 0;

-- name: hashsetop_except
-- FIX-2 (hashed) + FIX-7: EXCEPT plans as "HashSetOp Except" over two synthetic
-- Subquery Scan children whose aliases "*SELECT* 1"/"*SELECT* 2" must be quoted.
select id from ta except select id from tb;

-- name: setop_except_sorted
-- setup: set local enable_hashagg = off
-- FIX-2 (sorted): the same EXCEPT with hashing disabled plans as "SetOp Except".
select id from ta except select id from tb;

-- name: subquery_scan_keyword_alias
-- quoteIdentifier() keyword coverage: the subquery alias "left" is a lowercase
-- TYPE_FUNC_NAME keyword, so psql quote_identifier()s it ("Subquery Scan on
-- \"left\"") even though it matches the safe-identifier pattern; joe must quote it
-- too. Unlike the "*SELECT* N" SetOp aliases above (which quote via special
-- characters), this pins the non-UNRESERVED keyword branch that joe began quoting
-- once reservedKeywords grew into nonUnreservedKeywords. The outer "id > 0" (removes
-- 0 rows) keeps a Subquery Scan with a zero-removed Filter instead of flattening it.
select * from (select id from cats order by id limit 10) "left" where id > 0;

-- name: function_scan_single
-- FIX-3 + FIX-4 control: single Function Scan renders "Function Call:" and the
-- "on pg_catalog.generate_series g" caption.
select * from generate_series(1, 5) g;

-- name: function_scan_rows_from
-- FIX-3 (multi) + FIX-4: ROWS FROM carries no "Function Name", only the alias, so
-- the caption must fall back to "Function Scan on t" and join the two calls.
select * from rows from (generate_series(1, 5), generate_series(1, 3)) as t(a, b);

-- name: result_one_time_filter_const
-- FIX-5 (constant form): "where 1=0" yields a Result with "One-Time Filter: false".
select * from items where 1 = 0;

-- name: result_one_time_filter_expr
-- FIX-5 (expression form): the correlated subquery becomes an InitPlan and a
-- "One-Time Filter: ((InitPlan 1).col1 > 0)" (pg16 renders "$0"); Filter removes 18.
select * from cats where (select count(*) from items) > 0 and id < 3;

-- name: tid_scan_eq
-- setup: set local enable_seqscan = off; set local enable_indexscan = off; set local enable_bitmapscan = off
-- FIX-6: Tid Scan with an equality "TID Cond: (tidtest.ctid = '(0,1)'::tid)".
select ctid, * from tidtest where ctid = '(0,1)';

-- name: tid_range_scan
-- setup: set local enable_seqscan = off
-- FIX-6: Tid Range Scan with a range "TID Cond: (tidtest.ctid < '(2,0)'::tid)".
select * from tidtest where ctid < '(2,0)';

-- name: index_scan
-- Parity control: plain Index Scan with an "Index Cond". Its per-run buffer split
-- (hit vs read) differs between the two captures, exercising the Buffers mask.
select * from big where id = 12345;

-- name: merge_join
-- setup: set local enable_hashjoin = off; set local enable_nestloop = off
-- Parity control: Merge Join over two sorted inputs. Also surfaces the allowlisted
-- "Inner Unique:" line joe does not yet render (#210).
select * from cats c1 join cats c2 on c1.id = c2.id;

-- name: hash_join
-- Parity control: Hash Join + Hash node (Buckets/Batches/Memory Usage). Also emits
-- the allowlisted "Inner Unique:" line (#210).
select c.id from cats c join items i on i.cat = c.id;

-- name: sort_in_memory
-- Parity control: in-memory quicksort ("Sort Method: quicksort  Memory: NkB").
select * from items order by val;

-- name: aggregate_hash
-- Parity control: HashAggregate with a Group Key.
select cat, count(*) from items group by cat;

-- name: external_merge_sort
-- setup: set local work_mem = '64kB'
-- #210 Sort-Method fixture: a tiny work_mem forces "Sort Method: external merge
-- Disk: NkB", confirming joe renders the disk-spill sort method at parity.
select * from big order by val;

-- name: parallel_aggregate
-- setup: set local parallel_setup_cost = 0; set local parallel_tuple_cost = 0; set local min_parallel_table_scan_size = 0; set local max_parallel_workers_per_gather = 2
-- #210 per-worker fixture: a Gather over a Parallel Seq Scan. psql prints per-worker
-- "Worker N:" blocks joe does not render; the guard allowlists those and masks the
-- run-variable "Workers Launched:" count, confirming the rest is at parity.
select count(*) from big;
