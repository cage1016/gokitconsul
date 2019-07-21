package endpoints

import "github.com/cage1016/gokitconsul/pkg/authn/model"

type Request interface {
	validate() error
}

// LoginRequest collects the request parameters for the Login method.
type LoginRequest struct {
	User model.User `json:"user"`
}

func (r LoginRequest) validate() error {
	return r.User.Validate()
}

// LogoutRequest collects the request parameters for the Logout method.
type LogoutRequest struct {
	User model.User `json:"user"`
}

func (r LogoutRequest) validate() error {
	return r.User.Validate()
}

// AddRequest collects the request parameters for the Add method.
type AddRequest struct {
	User model.User `json:"user"`
}

func (r AddRequest) validate() error {
	return r.User.Validate()
}

// BatchAddRequest collects the request parameters for the BatchAdd method.
type BatchAddRequest struct {
	Users []model.User `json:"users"`
}

func (r BatchAddRequest) validate() error {
	for _, u := range r.Users {
		if err := u.Validate(); err != nil {
			return err
		}
	}
	return nil
}
