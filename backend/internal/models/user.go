package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// CollectionUsers is the MongoDB collection name. Keeping it beside the struct
// means the name is declared once instead of being typed as a string literal
// wherever the collection is opened.
const CollectionUsers = "users"

type User struct {
	ID bson.ObjectID `bson:"_id,omitempty" json:"id"`

	// Email is always stored lowercased and trimmed so the unique index
	// actually prevents "Bob@x.com" and "bob@x.com" being two accounts.
	Email string `bson:"email" json:"email"`

	// PasswordHash is a bcrypt hash. The json:"-" tag is the single most
	// important line in this file: it makes it impossible to accidentally
	// serialise the hash by returning a *User from a handler.
	PasswordHash string `bson:"passwordHash" json:"-"`

	CreatedAt time.Time `bson:"createdAt" json:"createdAt"`
}

// UserView is the shape the API returns. Having an explicit view type means
// adding a field to User never silently exposes it over HTTP.
type UserView struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"createdAt"`
}

func (u *User) View() UserView {
	return UserView{
		ID:        u.ID.Hex(),
		Email:     u.Email,
		CreatedAt: u.CreatedAt,
	}
}
