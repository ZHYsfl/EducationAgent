// Package ecodes — §0.2 与 Python error_codes 对齐。
package ecodes

const (
	OK               = 200
	Param            = 40001
	AuthRequired     = 40100
	AuthExpired      = 40101
	AuthInvalid      = 40102
	AuthUserMismatch = 40103
	AuthForbidden    = 40104
	NotFound         = 40400
	Conflict         = 40900
	Internal         = 50000
	Dependency       = 50200
)
