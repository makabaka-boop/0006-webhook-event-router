package domain

import "time"

// 事件状态流转常量。
const (
	EventReceived        = "received"
	EventAccepted        = "accepted"
	EventDelivered       = "delivered"
	EventPartiallyFailed = "partially_failed"
	EventFailed          = "failed"
	EventRejected        = "rejected"
	EventDuplicate       = "duplicate"
)

// 投递状态流转常量。
const (
	DeliveryPending   = "pending"
	DeliverySent      = "sent"
	DeliveryDelivered = "delivered"
	DeliveryFailed    = "failed"
	DeliveryRetrying  = "retrying"
	DeliveryDead      = "dead"
)

// Source 事件源。
type Source struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	Secret       string    `json:"-"`
	Enabled      bool      `json:"enabled"`
	AllowedTypes []string  `json:"allowed_event_types"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Event 事件。
type Event struct {
	ID           int64      `json:"id"`
	SourceID     int64      `json:"source_id"`
	EventType    string     `json:"event_type"`
	EventID      string     `json:"event_id"`
	Payload      string     `json:"payload"`
	Status       string     `json:"status"`
	ReceivedAt   time.Time  `json:"received_at"`
	CreatedAt    time.Time  `json:"created_at"`
	RejectReason string     `json:"reject_reason,omitempty"`
	Timestamp    *time.Time `json:"timestamp,omitempty"`
}

// Target 目标端点。
type Target struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Condition 规则条件。当 Op 为 and/or/not 时作为逻辑分组节点，
// 通过 Children 嵌套子条件；否则为叶子条件，用 Path 取值、Op 匹配、Value 比较。
type Condition struct {
	Op       string      `json:"op"`
	Path     string      `json:"path,omitempty"`
	Value    any         `json:"value,omitempty"`
	Children []Condition `json:"children,omitempty"`
}

// Rule 路由规则。TargetID 为兼容单目标场景的主目标；
// TargetIDs 为规则命中的全部目标（含 TargetID），支持多目标分发。
type Rule struct {
	ID        int64       `json:"id"`
	Name      string      `json:"name"`
	SourceID  *int64      `json:"source_id"`
	EventType string      `json:"event_type"`
	Condition []Condition `json:"condition"`
	TargetID  int64       `json:"target_id"`
	TargetIDs []int64     `json:"target_ids,omitempty"`
	Priority  int         `json:"priority"`
	Enabled   bool        `json:"enabled"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

// Delivery 投递记录。
type Delivery struct {
	ID          int64      `json:"id"`
	EventID     int64      `json:"event_id"`
	RuleID      int64      `json:"rule_id"`
	TargetID    int64      `json:"target_id"`
	Status      string     `json:"status"`
	Attempts    int        `json:"attempts"`
	NextRetryAt *time.Time `json:"next_retry_at,omitempty"`
	LastError   string     `json:"last_error,omitempty"`
	DeadAt      *time.Time `json:"dead_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// DeliveryAttempt 投递尝试记录。
type DeliveryAttempt struct {
	ID             int64     `json:"id"`
	DeliveryID     int64     `json:"delivery_id"`
	Status         string    `json:"status"`
	RequestBody    string    `json:"request_body"`
	ResponseStatus int       `json:"response_status"`
	ResponseBody   string    `json:"response_body"`
	Error          string    `json:"error,omitempty"`
	StartedAt      time.Time `json:"started_at"`
	FinishedAt     time.Time `json:"finished_at"`
}

// ChangeLog 变更日志。
type ChangeLog struct {
	ID         int64     `json:"id"`
	EntityType string    `json:"entity_type"`
	EntityID   int64     `json:"entity_id"`
	Action     string    `json:"action"`
	Before     string    `json:"before,omitempty"`
	After      string    `json:"after,omitempty"`
	Actor      string    `json:"actor,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}
