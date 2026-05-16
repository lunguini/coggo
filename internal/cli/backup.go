package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	cli "github.com/urfave/cli/v3"

	"github.com/lunguini/coggo/internal/config"
)

type identityExportResult struct {
	Source      string
	Destination string
	Bytes       int64
}

func cmdBackup() *cli.Command {
	return &cli.Command{
		Name:  "backup",
		Usage: "Backup Coggo data and identity material",
		Commands: []*cli.Command{
			{
				Name:  "identity",
				Usage: "Backup hosted peer identities",
				Commands: []*cli.Command{
					{
						Name:      "export",
						Usage:     "Export hosted peer identities to a private peers.json backup",
						ArgsUsage: "<path>",
						Flags: []cli.Flag{
							&cli.BoolFlag{
								Name:  "force",
								Usage: "Overwrite an existing destination file",
							},
						},
						Action: actionBackupIdentityExport,
					},
					{
						Name:      "import",
						Usage:     "Import hosted peer identities from a peers.json backup",
						ArgsUsage: "<path>",
						Flags: []cli.Flag{
							&cli.BoolFlag{
								Name:  "force",
								Usage: "Overwrite an existing local peers.json",
							},
						},
						Action: actionBackupIdentityImport,
					},
				},
			},
		},
	}
}

func actionBackupIdentityExport(ctx context.Context, cmd *cli.Command) error {
	_ = ctx
	dest := cmd.Args().First()
	if dest == "" {
		return fmt.Errorf("backup identity export: destination path required")
	}
	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}
	result, err := exportIdentityBackup(config.DataDir(cfg), config.ExpandPath(dest), cmd.Bool("force"))
	if err != nil {
		return err
	}
	fmt.Printf("Exported Coggo identities to %s\n", result.Destination)
	fmt.Printf("Source: %s\n", result.Source)
	fmt.Printf("Bytes:  %d\n", result.Bytes)
	fmt.Println("Keep this file encrypted or in a password manager; it contains peer private keys.")
	return nil
}

func actionBackupIdentityImport(ctx context.Context, cmd *cli.Command) error {
	_ = ctx
	src := cmd.Args().First()
	if src == "" {
		return fmt.Errorf("backup identity import: source path required")
	}
	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}
	result, err := importIdentityBackup(config.DataDir(cfg), config.ExpandPath(src), cmd.Bool("force"))
	if err != nil {
		return err
	}
	fmt.Printf("Imported Coggo identities from %s\n", result.Source)
	fmt.Printf("Destination: %s\n", result.Destination)
	fmt.Printf("Bytes:       %d\n", result.Bytes)
	fmt.Println("Restart any running coggo processes so they reload the peer registry.")
	return nil
}

func exportIdentityBackup(dataDir, dest string, force bool) (*identityExportResult, error) {
	src := filepath.Join(dataDir, "peers.json")
	if dest == "" {
		return nil, fmt.Errorf("backup identity export: destination path required")
	}
	result, err := copyIdentityFile(src, dest, force, "backup identity export")
	if err != nil {
		return nil, err
	}
	return result, nil
}

func importIdentityBackup(dataDir, src string, force bool) (*identityExportResult, error) {
	if src == "" {
		return nil, fmt.Errorf("backup identity import: source path required")
	}
	dest := filepath.Join(dataDir, "peers.json")
	result, err := copyIdentityFile(src, dest, force, "backup identity import")
	if err != nil {
		return nil, err
	}
	return result, nil
}

func copyIdentityFile(src, dest string, force bool, op string) (*identityExportResult, error) {
	srcInfo, err := os.Stat(src)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s: %s does not exist", op, src)
		}
		return nil, fmt.Errorf("%s: stat %s: %w", op, src, err)
	}
	if srcInfo.IsDir() {
		return nil, fmt.Errorf("%s: %s is a directory", op, src)
	}
	if srcInfo.Size() == 0 {
		return nil, fmt.Errorf("%s: %s is empty", op, src)
	}
	if err := ensureCanWriteDestination(dest, force, op); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return nil, fmt.Errorf("%s: create destination directory: %w", op, err)
	}
	in, err := os.Open(src)
	if err != nil {
		return nil, fmt.Errorf("%s: open %s: %w", op, src, err)
	}
	defer in.Close()

	tmp, err := os.CreateTemp(filepath.Dir(dest), "."+filepath.Base(dest)+".*.tmp")
	if err != nil {
		return nil, fmt.Errorf("%s: create temporary file: %w", op, err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if err := os.Chmod(tmpName, 0o600); err != nil {
		_ = tmp.Close()
		return nil, fmt.Errorf("%s: chmod temporary file: %w", op, err)
	}
	written, err := io.Copy(tmp, in)
	if err != nil {
		_ = tmp.Close()
		return nil, fmt.Errorf("%s: copy peers.json: %w", op, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return nil, fmt.Errorf("%s: sync temporary file: %w", op, err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("%s: close temporary file: %w", op, err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return nil, fmt.Errorf("%s: chmod backup: %w", op, err)
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return nil, fmt.Errorf("%s: move backup into place: %w", op, err)
	}
	cleanup = false
	return &identityExportResult{
		Source:      src,
		Destination: dest,
		Bytes:       written,
	}, nil
}

func ensureCanWriteDestination(dest string, force bool, op string) error {
	info, err := os.Lstat(dest)
	if err == nil {
		if info.IsDir() {
			return fmt.Errorf("%s: destination %s is a directory", op, dest)
		}
		if !force {
			return fmt.Errorf("%s: destination %s already exists; pass --force to overwrite", op, dest)
		}
		return nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("%s: stat destination %s: %w", op, dest, err)
}
