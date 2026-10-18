package domain

import (
	"time"

	"github.com/google/uuid"
)

// Entity contains state shared by persisted domain entities.
type Entity struct {
	ID        uuid.UUID `json:"id" bun:"id,pk,type:uuid"`
	CreatedAt time.Time `json:"created_at" bun:"created_at,notnull"`
	UpdatedAt time.Time `json:"updated_at" bun:"updated_at,notnull"`
}

func NewEntity() (Entity, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return Entity{}, err
	}
	now := time.Now().UTC()

	return Entity{
		ID:        id,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (e *Entity) Touch() {
	e.UpdatedAt = time.Now().UTC()
}
