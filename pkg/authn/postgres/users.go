package postgres

import (
	"context"
	"database/sql"
	"github.com/go-kit/kit/log"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"

	"github.com/cage1016/gokitconsul/pkg/authn/model"
	"github.com/cage1016/gokitconsul/pkg/authn/service"
)

var _ model.UserRepository = (*userRepository)(nil)

const (
	errDuplicate = "unique_violation"
	errInvalid   = "invalid_text_representation"
)

type userRepository struct {
	db  *sqlx.DB
	log log.Logger
}

// New instantiates a PostgreSQL implementation of user
// repository.
func New(db *sqlx.DB, log log.Logger) model.UserRepository {
	return &userRepository{db, log}
}

func (ur userRepository) Save(ctx context.Context, user model.User) error {
	q := `INSERT INTO users (email, password) VALUES (:email, :password)`

	dbu := model.User{
		Email:    user.Email,
		Password: user.Password,
	}
	if _, err := ur.db.NamedExec(q, dbu); err != nil {
		if pqErr, ok := err.(*pq.Error); ok && errDuplicate == pqErr.Code.Name() {
			return service.ErrConflict
		}
		return err
	}

	return nil
}

func (ur userRepository) RetrieveByID(ctx context.Context, email string) (model.User, error) {
	q := `SELECT password FROM users WHERE email = $1`
	dbu := model.User{
		Email: email,
	}

	if err := ur.db.QueryRowx(q, email).StructScan(&dbu); err != nil {
		if err == sql.ErrNoRows {
			return model.User{}, service.ErrNotFound
		}
		return model.User{}, err
	}

	return dbu, nil
}
