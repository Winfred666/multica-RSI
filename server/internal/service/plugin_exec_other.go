//go:build !linux && !darwin

package service

import (
	"context"
	"encoding/json"
)

func (s *PluginService) callHookExecutable(context.Context, HookInvocation) (json.RawMessage, error) {
	return nil, pluginErrf(PluginErrorIncompatible, "hosted plugin execution requires Linux or macOS")
}
