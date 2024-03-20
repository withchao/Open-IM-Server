// Copyright © 2023 OpenIM. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"context"
	"github.com/openimsdk/open-im-server/v3/internal/tools"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/util/genutil"
	"github.com/spf13/cobra"
)

type CronTaskCmd struct {
	*RootCmd
	initFunc func(ctx context.Context, config *config.GlobalConfig) error
	ctx      context.Context
}

func NewCronTaskCmd(name string) *CronTaskCmd {
	ret := &CronTaskCmd{RootCmd: NewRootCmd(genutil.GetProcessName(), name, WithCronTaskLogName()),
		initFunc: tools.StartTask}
	ret.ctx = context.WithValue(context.Background(), "version", config.Version)
	ret.addRunE()
	ret.SetRootCmdPt(ret)
	return ret
}

func (c *CronTaskCmd) addRunE() {
	c.Command.RunE = func(cmd *cobra.Command, args []string) error {
		return c.initFunc(c.ctx, c.config)
	}
}

func (c *CronTaskCmd) Exec() error {
	return c.Execute()
}

func (c *CronTaskCmd) GetPortFromConfig(portType string) int {
	return 0
}
