package contract

const (
	CodeSuccess                    = 200
	CodeBadRequest                 = 40001
	CodePasswordTooShort           = 40002
	CodeInvalidCredentials         = 40100
	CodeTokenExpired               = 40101
	CodeEmailNotVerified           = 40102
	CodeInvalidVerificationToken   = 40103
	CodeExpiredVerificationToken   = 40104
	CodeResourceNotFound           = 40400
	CodeConflictUsername           = 40901
	CodeConflictEmail              = 40902
	CodeVerificationTokenConsumed  = 40903
	CodeInternalError              = 50000
	CodeVerificationEmailSendError = 50201
)
