package store

import (
	"context"
	"errors"
	"time"

	"webhook-event-router/internal/domain"
)

// ErrNotFound 表明记录不存在。
var ErrNotFound = errors.New("not found")

// DeliveryOutcome 描述一次投递完成后的持久化结果。
type DeliveryOutcome struct {
	Status      string
	Attempts    int
	NextRetryAt *time.Time
	LastError   string
}

// Store 聚合各资源存储接口。
type Store interface {
	SourceStore
	RuleStore
	TargetStore
	EventStore
	DeliveryStore
	ChangeLogStore
	AuditStore
	RuleTargetStore
}

// SourceStore 事件源存储。
type SourceStore interface {
	CreateSource(ctx context.Context, s *domain.Source) error
	ListSources(ctx context.Context) ([]*domain.Source, error)
	GetSource(ctx context.Context, id int64) (*domain.Source, error)
	UpdateSource(ctx context.Context, s *domain.Source) error
	DeleteSource(ctx context.Context, id int64) error
}

// RuleStore 规则存储。
type RuleStore interface {
	CreateRule(ctx context.Context, r *domain.Rule) error
	ListRules(ctx context.Context) ([]*domain.Rule, error)
	GetRule(ctx context.Context, id int64) (*domain.Rule, error)
	UpdateRule(ctx context.Context, r *domain.Rule) error
	DeleteRule(ctx context.Context, id int64) error
	ListEnabledRules(ctx context.Context) ([]*domain.Rule, error)
}

// TargetStore 目标存储。
type TargetStore interface {
	CreateTarget(ctx context.Context, t *domain.Target) error
	ListTargets(ctx context.Context) ([]*domain.Target, error)
	GetTarget(ctx context.Context, id int64) (*domain.Target, error)
	UpdateTarget(ctx context.Context, t *domain.Target) error
	DeleteTarget(ctx context.Context, id int64) error
}

// EventStore 事件存储。
type EventStore interface {
	CreateEvent(ctx context.Context, e *domain.Event) error
	GetEvent(ctx context.Context, id int64) (*domain.Event, error)
	GetByKey(ctx context.Context, sourceID int64, eventID string) (*domain.Event, error)
	UpdateStatus(ctx context.Context, id int64, status string, reason string) error
}

// DeliveryStore 投递存储。
type DeliveryStore interface {
	CreateDelivery(ctx context.Context, d *domain.Delivery) error
	GetDelivery(ctx context.Context, id int64) (*domain.Delivery, error)
	ListByEvent(ctx context.Context, eventID int64) ([]*domain.Delivery, error)
	ListDeliveries(ctx context.Context, eventID int64, status string, page, limit int) ([]*domain.Delivery, error)
	UpdateDelivery(ctx context.Context, d *domain.Delivery) error
	ApplyDeliveryOutcome(ctx context.Context, id int64, outcome DeliveryOutcome) error
	DueDeliveries(ctx context.Context, now int64, limit int) ([]*domain.Delivery, error)
}

// ChangeLogStore 变更日志存储。
type ChangeLogStore interface {
	CreateChangeLog(ctx context.Context, cl *domain.ChangeLog) error
}

// AuditStore 审计查询接口。
type AuditStore interface {
	ListChangeLogs(ctx context.Context, entityType string, entityID int64, page, limit int) ([]*domain.ChangeLog, error)
}

// RuleTargetStore 规则-目标关联存储（多目标规则）。
type RuleTargetStore interface {
	CreateRuleTargets(ctx context.Context, ruleID int64, targetIDs []int64) error
	ListRuleTargets(ctx context.Context, ruleID int64) ([]int64, error)
	DeleteRuleTargets(ctx context.Context, ruleID int64) error
}
