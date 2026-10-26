package usecase

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/bahr/secunda/internal/entity"
)

func validateText(value, field string, max int, required bool) error {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > max || (required && strings.TrimSpace(value) == "") {
		min := 0
		if required {
			min = 1
		}
		return fmt.Errorf("%w: %s must contain %d–%d characters", entity.ErrInvalid, field, min, max)
	}
	return nil
}
