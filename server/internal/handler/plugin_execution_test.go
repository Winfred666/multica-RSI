package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

func TestPluginExecutionSettingsAuthorizationAndSecrecy(t *testing.T) {
	withPluginsV1Flag(t, testHandler, true)
	previousBox, previousURL := testHandler.PluginService.Secrets, testHandler.PluginService.ServerURL
	t.Cleanup(func() {
		testHandler.PluginService.Secrets, testHandler.PluginService.ServerURL = previousBox, previousURL
	})
	box, err := secretbox.New(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	testHandler.PluginService.Secrets, testHandler.PluginService.ServerURL = box, "https://example.com"
	workspace := dbfx.Workspace(t, "Plugin execution", "exec-"+uuid.NewString())
	dbfx.Member(t, workspace, testUserID, "owner")
	alice := dbfx.User(t, "Alice", uuid.NewString()+"@example.com")
	dbfx.Member(t, workspace, alice, "member")
	token, err := auth.GeneratePATToken()
	if err != nil {
		t.Fatal(err)
	}
	dbfx.Insert(t, "personal_access_token", testutil.Cols{"user_id": alice, "name": "Plugin PAT", "token_hash": auth.HashToken(token), "token_prefix": token[:12]})
	dbfx.Cleanup(t, `DELETE FROM plugin_workspace_execution WHERE workspace_id = $1`, workspace)
	request := func(method string, body []byte) *http.Request {
		return pluginHandlerRequest(method, "/api/workspaces/"+workspace+"/plugins/execution", body, map[string]string{"id": workspace})
	}
	var status service.PluginExecutionStatus
	testutil.Call(t, testHandler.GetPluginExecution, request(http.MethodGet, nil)).Want(http.StatusOK).JSON(&status)
	if status.Configured || status.Enabled {
		t.Fatal("execution enabled without consent")
	}
	body, _ := json.Marshal(map[string]string{"token": token})
	memberRequest := request(http.MethodPut, body)
	memberRequest.Header.Set("X-User-ID", alice)
	testutil.Call(t, testHandler.SetPluginExecution, memberRequest).Want(http.StatusForbidden)
	response := testutil.Call(t, testHandler.SetPluginExecution, request(http.MethodPut, body)).Want(http.StatusOK).JSON(&status)
	if !status.Enabled || status.RunAs == nil || status.RunAs.ID != alice {
		t.Fatal("API did not return chosen owner")
	}
	if strings.Contains(response.Body.String(), token) || strings.Contains(response.Body.String(), "ciphertext") {
		t.Fatal("write response leaked credential")
	}
	response = testutil.Call(t, testHandler.GetPluginExecution, request(http.MethodGet, nil)).Want(http.StatusOK)
	if strings.Contains(response.Body.String(), token) || strings.Contains(response.Body.String(), "ciphertext") {
		t.Fatal("read response leaked credential")
	}
	testutil.Call(t, testHandler.SetPluginExecution, request(http.MethodPut, []byte(`{"token":""}`))).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.DeletePluginExecution, request(http.MethodDelete, nil)).Want(http.StatusNoContent)
	testutil.Call(t, testHandler.GetPluginExecution, request(http.MethodGet, nil)).Want(http.StatusOK).JSON(&status)
	if status.Configured || status.Enabled {
		t.Fatal("disconnect left execution enabled")
	}
	withPluginsV1Flag(t, testHandler, false)
	testutil.Call(t, testHandler.SetPluginExecution, request(http.MethodPut, body)).Want(http.StatusForbidden)
}
