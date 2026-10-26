// Package usecase contains application rules. Its ports are implemented by outer adapters.
package usecase

import (
	"context"

	"github.com/bahr/secunda/internal/entity"
)

type UserRepository interface {
	Create(context.Context, *entity.User) error
	ByEmail(context.Context, string) (entity.User, error)
}

type MembershipReader interface {
	Membership(context.Context, int64, int64) (entity.Membership, error)
}

type TeamRepository interface {
	MembershipReader
	List(context.Context, int64, entity.Pagination) ([]entity.Team, error)
	Stats(context.Context, int64) (entity.TeamStats, error)
}

// TaskListAccess is a read model for a team member's task list. Revision belongs
// to this list, not to Membership. Missing membership must return ErrNotFound.
type TaskListAccess struct {
	Membership entity.Membership
	Revision   int64
}

type TaskRepository interface {
	ByID(context.Context, int64) (entity.Task, error)
	ListAccess(context.Context, int64, int64) (TaskListAccess, error)
	List(context.Context, entity.TaskFilter) ([]entity.Task, error)
	History(context.Context, int64, entity.Pagination) ([]entity.History, error)
	AddComment(context.Context, *entity.Comment) error
	Comments(context.Context, int64, entity.Pagination) ([]entity.Comment, error)
}

// TeamWriter is available only inside Within. Create also saves the owner's
// membership. AddMember must reject missing users and duplicate membership.
type TeamWriter interface {
	MembershipReader
	Create(context.Context, *entity.Team) error
	AddMember(context.Context, entity.Membership) error
	ChangeRole(context.Context, entity.Membership) error
}

// TaskWriter is available only inside Within. Every write saves its history and
// advances the team's list revision. Update requires the state read by ForUpdate.
type TaskWriter interface {
	ForUpdate(context.Context, int64) (entity.Task, error)
	Create(context.Context, *entity.Task) error
	Update(ctx context.Context, previous, updated entity.Task, actor int64) error
}

type Transaction interface {
	Teams() TeamWriter
	Tasks() TaskWriter
}

// Within commits all operations only when the callback returns nil. A callback
// error or panic rolls back every write; transaction capabilities must not escape.
type Transactor interface {
	Within(context.Context, func(Transaction) error) error
}

type PasswordHasher interface {
	Hash(string) (string, error)
	Compare(hash, password string) bool
}

type TokenIssuer interface {
	Issue(userID int64) (string, error)
}

// Revision is stored in MySQL and changes in the same transaction as tasks.
// Old cache entries are unreachable after commit, even during Redis outages.
type TaskCache interface {
	Get(context.Context, entity.TaskFilter, int64) (entity.TaskPage, bool)
	Set(context.Context, entity.TaskFilter, int64, entity.TaskPage)
}
