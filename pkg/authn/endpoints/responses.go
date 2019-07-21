package endpoints

import (
	"net/http"

	httptransport "github.com/go-kit/kit/transport/http"
)

var (
	_ httptransport.Headerer = (*LoginResponse)(nil)

	_ httptransport.StatusCoder = (*LoginResponse)(nil)

	_ httptransport.Headerer = (*LogoutResponse)(nil)

	_ httptransport.StatusCoder = (*LogoutResponse)(nil)

	_ httptransport.Headerer = (*AddResponse)(nil)

	_ httptransport.StatusCoder = (*AddResponse)(nil)

	_ httptransport.Headerer = (*BatchAddResponse)(nil)

	_ httptransport.StatusCoder = (*BatchAddResponse)(nil)
)

// LoginResponse collects the response values for the Login method.
type LoginResponse struct {
	Token string `json:"token"`
	Err   error  `json:"err"`
}

func (r LoginResponse) StatusCode() int {
	return http.StatusOK // TBA
}

func (r LoginResponse) Headers() http.Header {
	return http.Header{}
}

// LogoutResponse collects the response values for the Logout method.
type LogoutResponse struct {
	Err error `json:"err"`
}

func (r LogoutResponse) StatusCode() int {
	return http.StatusNoContent // TBA
}

func (r LogoutResponse) Headers() http.Header {
	return http.Header{}
}

// AddResponse collects the response values for the Add method.
type AddResponse struct {
	Err error `json:"err"`
}

func (r AddResponse) StatusCode() int {
	return http.StatusCreated // TBA
}

func (r AddResponse) Headers() http.Header {
	return http.Header{}
}

// BatchAddResponse collects the response values for the BatchAdd method.
type BatchAddResponse struct {
	Err error `json:"err"`
}

func (r BatchAddResponse) StatusCode() int {
	return http.StatusCreated // TBA
}

func (r BatchAddResponse) Headers() http.Header {
	return http.Header{}
}
