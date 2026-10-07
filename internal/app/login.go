package app

import (
	"context"
	"fmt"
	"io"
)

// Identity belongs to the launcher. Products only consume its shared session.
func login(_ context.Context, _ io.Reader, _ io.Writer) error {
	return fmt.Errorf("authentication is shared across HIVE products; run hive login")
}
