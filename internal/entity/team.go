package entity

import "time"

type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

func (r Role) CanManage() bool { return r == RoleOwner || r == RoleAdmin }

type Team struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedBy int64     `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	Role      Role      `json:"role"`
}

type Membership struct {
	TeamID int64 `json:"team_id"`
	UserID int64 `json:"user_id"`
	Role   Role  `json:"role"`
}
