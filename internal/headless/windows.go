//go:build windows

package headless

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/endless-net/client/internal/localipc"
	"github.com/google/uuid"
	"golang.org/x/sys/windows"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// OpenWindows reads only the public instance hint. Authentication comes from
// the actual SYSTEM-owned named pipe and the exact published SDK descriptor.
func OpenWindows(ctx context.Context, digest [32]byte) (*grpc.ClientConn, error) {
	common, err := windows.KnownFolderPath(windows.FOLDERID_ProgramData, 0)
	if err != nil {
		return nil, errors.New("не найден системный каталог Client")
	}
	file, err := os.Open(filepath.Join(common, "EndlessNet", "Client", "runtime-instance"))
	if err != nil {
		return nil, errors.New("клиент не установлен или служба не запущена")
	}
	data, readErr := io.ReadAll(io.LimitReader(file, 129))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(data) > 128 {
		return nil, errors.New("некорректный указатель локальной службы Client")
	}
	instance := strings.TrimSpace(string(data))
	id, err := uuid.Parse(instance)
	if err != nil || id == uuid.Nil || id.String() != instance {
		return nil, errors.New("некорректный идентификатор локальной службы Client")
	}
	channel, err := localipc.Connect(ctx, localipc.PipeConfig{
		InstanceID: instance, ServerSID: "S-1-5-18", ContractDigest: digest,
		HandshakeTimeout: 2 * time.Second,
	})
	if err != nil {
		if status.Code(err) == codes.FailedPrecondition {
			return nil, errors.New("версии службы и консольного клиента не совпадают; требуется согласованная установка")
		}
		return nil, errors.New("не удалось подключиться к доверенной локальной службе Client")
	}
	return channel, nil
}
