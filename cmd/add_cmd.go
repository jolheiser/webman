package cmd

import (
	"webman/cmd/add"
	"webman/cmd/dev"
	"webman/cmd/group"
	"webman/cmd/remove"
	"webman/cmd/run"
	"webman/cmd/search"
	switchcmd "webman/cmd/switch"
	"webman/cmd/version"
)

func init() {
	rootCmd.AddCommand(add.AddCmd)
	rootCmd.AddCommand(dev.DevCmd)
	rootCmd.AddCommand(remove.RemoveCmd)
	rootCmd.AddCommand(run.RunCmd)
	rootCmd.AddCommand(switchcmd.SwitchCmd)
	rootCmd.AddCommand(group.GroupCmd)
	rootCmd.AddCommand(search.SearchCmd)
	rootCmd.AddCommand(version.VersionCmd)
}
