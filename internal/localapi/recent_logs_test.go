package localapi

import (
	"context"
	"testing"

	clientv0 "github.com/endless-net/client/api/client/v0"
	"github.com/endless-net/client/internal/localipc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRecentLogsOwnerPaginationAndEviction(t *testing.T) {
	c, _, _, now := fixture(t)
	ctx := context.Background()
	caller := localipc.Principal{SID: "S-1-5-21-11"}
	if _, err := c.begin(ctx, caller, request(1)); err != nil {
		t.Fatal(err)
	}
	logs := []RecentLog{
		{Sequence: 1, Time: *now, Message: "one"},
		{Sequence: 2, Time: *now, Message: "two"},
	}
	c.config.RecentLogs = func() []RecentLog { return logs }
	c.config.LogPageKey = [32]byte{1}
	c.config.LogResponseBytes = 65536
	req := &clientv0.ListRecentLogsRequest{Page: &clientv0.PageRequest{PageSize: 1}}
	if _, err := c.listRecentLogs(ctx, localipc.Principal{SID: "S-1-5-21-12", Administrator: true}, req); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("foreign administrator read logs: %v", err)
	}
	page, err := c.listRecentLogs(ctx, caller, req)
	if err != nil || len(page.GetLogs()) != 1 || page.Logs[0].Message != "one" || page.GetPage().GetNextPageToken() == "" {
		t.Fatalf("first page: %v, %v", page, err)
	}
	req.Page.PageToken = page.Page.NextPageToken
	logs = append(logs, RecentLog{Sequence: 3, Time: *now, Message: "three"})
	page, err = c.listRecentLogs(ctx, caller, req)
	if err != nil || len(page.GetLogs()) != 1 || page.Logs[0].Message != "two" || page.GetPage().GetNextPageToken() != "" {
		t.Fatalf("second page escaped the original snapshot: %v, %v", page, err)
	}
	logs = logs[2:]
	if _, err := c.listRecentLogs(ctx, caller, req); status.Code(err) != codes.Aborted {
		t.Fatalf("expired retained window was accepted: %v", err)
	}
}
