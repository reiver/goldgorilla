package models

type BigError struct {
	message  string `json:"message"`
	errCode  int    `json:"-"`
	metaData any    `json:"meta"`
}

func (err BigError) Error() string {
	return err.message
}

func (err BigError) ErrCode() int {
	return err.errCode
}

func (err BigError) Meta() any {
	return err.metaData
}

func NewError(msg string, errCode int, meta any) error {
	if meta == nil {
		meta = struct {
		}{}
	}
	return BigError{
		message:  msg,
		errCode:  errCode,
		metaData: meta,
	}
}

func (BigError) FromErr(err error, errCode int, meta any) error {
	if meta == nil {
		meta = err
	}
	return BigError{
		message:  err.Error(),
		errCode:  errCode,
		metaData: meta,
	}
}
