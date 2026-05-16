package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	cli "github.com/urfave/cli/v3"
)

func TestUnknownTopLevelCommandReturnsUsageError(t *testing.T) {
	err := App().Run(context.Background(), []string{"coggo", "identity", "backup", "export"})
	if err == nil {
		t.Fatal("App().Run succeeded, want unknown command error")
	}
	var exitErr cli.ExitCoder
	if !errors.As(err, &exitErr) {
		t.Fatalf("error = %T %q, want cli.ExitCoder", err, err)
	}
	if exitErr.ExitCode() != 2 {
		t.Fatalf("exit code = %d, want 2", exitErr.ExitCode())
	}
	if !strings.Contains(err.Error(), `Error: unknown command "identity"`) {
		t.Fatalf("error = %q, want unknown command context", err)
	}
	if !strings.Contains(err.Error(), "coggo --help") {
		t.Fatalf("error = %q, want help suggestion", err)
	}
}
