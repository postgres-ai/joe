/*
2026 © Postgres.ai
*/

package dblabapi

// Clone describes a DBLab clone.
type Clone struct {
	ID        string        `json:"id"`
	Snapshot  *Snapshot     `json:"snapshot"`
	Protected bool          `json:"protected"`
	DeleteAt  string        `json:"deleteAt"`
	CreatedAt string        `json:"createdAt"`
	Status    Status        `json:"status"`
	DB        Database      `json:"db"`
	Metadata  CloneMetadata `json:"metadata"`
}

// CloneMetadata describes clone properties reported by DBLab.
type CloneMetadata struct {
	CloneDiffSize  uint64  `json:"cloneDiffSize"`
	LogicalSize    uint64  `json:"logicalSize"`
	CloningTime    float64 `json:"cloningTime"`
	MaxIdleMinutes uint    `json:"maxIdleMinutes"`
}

// Database describes the connection parameters of a clone.
type Database struct {
	ConnStr  string `json:"connStr"`
	Host     string `json:"host"`
	Port     string `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	DBName   string `json:"dbName"`
}

// Snapshot describes the snapshot a clone is created from.
type Snapshot struct {
	ID           string `json:"id"`
	CreatedAt    string `json:"createdAt"`
	DataStateAt  string `json:"dataStateAt"`
	PhysicalSize uint64 `json:"physicalSize"`
	LogicalSize  uint64 `json:"logicalSize"`
	Pool         string `json:"pool"`
	NumClones    int    `json:"numClones"`
}

// Status describes the state of a clone.
type Status struct {
	Code    StatusCode `json:"code"`
	Message string     `json:"message"`
}

// StatusCode is a clone state code.
type StatusCode string

// Clone status codes.
const (
	StatusOK        StatusCode = "OK"
	StatusCreating  StatusCode = "CREATING"
	StatusResetting StatusCode = "RESETTING"
	StatusDeleting  StatusCode = "DELETING"
	StatusFatal     StatusCode = "FATAL"
)

// ErrorCode is a DBLab API error code.
type ErrorCode string

// DBLab API error codes.
const (
	ErrCodeInternal     ErrorCode = "INTERNAL_ERROR"
	ErrCodeBadRequest   ErrorCode = "BAD_REQUEST"
	ErrCodeUnauthorized ErrorCode = "UNAUTHORIZED"
	ErrCodeNotFound     ErrorCode = "NOT_FOUND"
)

// Error is an error response of the DBLab API.
type Error struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

// Error returns the error message.
func (e Error) Error() string {
	return e.Message
}

// CloneCreateRequest is the body of a clone create request.
type CloneCreateRequest struct {
	ID        string           `json:"id"`
	Protected bool             `json:"protected"`
	DB        *DatabaseRequest `json:"db"`
}

// DatabaseRequest describes the database user of a new clone.
type DatabaseRequest struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	Restricted bool   `json:"restricted"`
	DBName     string `json:"db_name"`
}

// ResetCloneRequest is the body of a clone reset request.
type ResetCloneRequest struct {
	SnapshotID string `json:"snapshotID"`
	Latest     bool   `json:"latest"`
}
