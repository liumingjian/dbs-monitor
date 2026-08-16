package httpapi

import "github.com/liumingjian/dbs-monitor/internal/api"

type fieldError struct {
	field   string
	message string
}

func validationErrorBody(message string, fieldErrors []fieldError) api.Error {
	body := errorBody(api.VALIDATIONFAILED, message)
	responseErrors := make([]struct {
		Field   string `json:"field"`
		Message string `json:"message"`
	}, len(fieldErrors))
	for index, item := range fieldErrors {
		responseErrors[index].Field = item.field
		responseErrors[index].Message = item.message
	}
	body.Error.FieldErrors = &responseErrors
	return body
}
