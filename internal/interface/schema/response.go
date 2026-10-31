package schema

import "smapass-back/internal/domain/entity"

type Health struct {
	Status string `json:"status"`
}

type Error struct {
	Error ErrorDetail `json:"error"`
}
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Certificates struct {
	Items []entity.Certificate `json:"items"`
}

type Records struct {
	Items  []entity.VerificationRecord `json:"items"`
	Total  int                         `json:"total"`
	Limit  int                         `json:"limit"`
	Offset int                         `json:"offset"`
}
