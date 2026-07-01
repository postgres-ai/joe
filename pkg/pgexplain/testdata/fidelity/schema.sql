-- Deterministic seed schema for the pgexplain byte-fidelity battery
-- (pkg/pgexplain/fidelity_test.go, TestFidelityAgainstLiveServer).
--
-- Row counts and values are fixed so that plan shapes are stable across runs and
-- servers: every Filter/HAVING removes a known (non-)zero count, the EXCEPT has
-- overlapping keys, tidtest has predictable ctids, and `big` is large enough to
-- force an external merge sort and a parallel scan once the relevant costs are
-- zeroed. The battery re-seeds from scratch, so the DROP is unconditional.

DROP TABLE IF EXISTS items, cats, ta, tb, tidtest, big CASCADE;

-- items: 10 categories x 200 rows (2000 total), val = 1..2000.
--  * `val > 5`            removes exactly 5 rows  (FIX-1 >0 control)
--  * GROUP BY cat HAVING  every group has 200 rows (FIX-1 =0 control)
CREATE TABLE items (cat int NOT NULL, val int NOT NULL);
INSERT INTO items SELECT g % 10, g FROM generate_series(1, 2000) g;
CREATE INDEX items_val_idx ON items (val);
ANALYZE items;

-- cats: 20 rows, integer primary key (drives merge join + a correlated Result).
CREATE TABLE cats (id int PRIMARY KEY);
INSERT INTO cats SELECT generate_series(1, 20);
ANALYZE cats;

-- ta/tb: overlapping id sets so `ta EXCEPT tb` keeps {1,2} and the SetOp reads
-- two synthetic "*SELECT* N" Subquery Scan children (FIX-2 + FIX-7).
CREATE TABLE ta (id int);
CREATE TABLE tb (id int);
INSERT INTO ta SELECT generate_series(1, 5);
INSERT INTO tb SELECT generate_series(3, 7);
ANALYZE ta;
ANALYZE tb;

-- tidtest: 100 rows, small enough to keep a single heap page so ctid '(0,1)'
-- and the range '< (2,0)' are stable (FIX-6).
CREATE TABLE tidtest (id int, filler text);
INSERT INTO tidtest SELECT g, 'row-' || g FROM generate_series(1, 100) g;
ANALYZE tidtest;

-- big: 50k rows. val is a scattered permutation so ORDER BY val needs a real
-- sort (external merge under a tiny work_mem), and the table is large enough to
-- go parallel once parallel_setup_cost / min_parallel_table_scan_size are zeroed.
CREATE TABLE big (id int, val int);
INSERT INTO big SELECT g, (g * 2654435761)::bigint % 100000 FROM generate_series(1, 50000) g;
CREATE INDEX big_id_idx ON big (id);
ANALYZE big;
