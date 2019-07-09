package endpoints

import "net/http"

var (
	_ Response = (*FooResponse)(nil)
)

// Response contains HTTP response specific methods.
type Response interface {
	Code() int
	Headers() map[string]string
	Empty() bool
	Error() error
}

// FooResponse collects the response values for the Foo method.
type FooResponse struct {
	Res string `json:"res"`
	Err error  `json:"err"`
}

func (r FooResponse) Code() int {
	return http.StatusOK // TBA
}

func (r FooResponse) Headers() map[string]string {
	return map[string]string{} // TBA
}

func (r FooResponse) Empty() bool {
	return false // TBA
}

func (r FooResponse) Error() error {
	return r.Err
}