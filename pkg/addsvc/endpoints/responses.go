package endpoints

import "net/http"

var (
	_ Response = (*SumResponse)(nil)
	_ Response = (*ConcatResponse)(nil)
)

// Response contains HTTP response specific methods.
type Response interface {
	Code() int
	Headers() map[string]string
	Empty() bool
	Error() error
}

// SumResponse collects the response values for the Sum method.
type SumResponse struct {
	Rs  int64 `json:"rs"`
	Err error `json:"err"`
}

func (r SumResponse) Code() int {
	return http.StatusOK // TBA
}

func (r SumResponse) Headers() map[string]string {
	return map[string]string{} // TBA
}

func (r SumResponse) Empty() bool {
	return false // TBA
}

func (r SumResponse) Error() error {
	return r.Err
}

// ConcatResponse collects the response values for the Concat method.
type ConcatResponse struct {
	Rs  string `json:"rs"`
	Err error  `json:"err"`
}

func (r ConcatResponse) Code() int {
	return http.StatusOK // TBA
}

func (r ConcatResponse) Headers() map[string]string {
	return map[string]string{} // TBA
}

func (r ConcatResponse) Empty() bool {
	return false // TBA
}

func (r ConcatResponse) Error() error {
	return r.Err
}

