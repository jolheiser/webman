package cmd

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"webman/multiline"
	"webman/pkgparse"
	"webman/unpack"

	"github.com/fatih/color"

	progressbar "github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
)

// addCmd represents the add command
var addCmd = &cobra.Command{
	Use:   "add",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	Run: func(cmd *cobra.Command, args []string) {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			panic(err)
		}
		webmanDir := filepath.Join(homeDir, "/.webman")
		webmanPkgDir := filepath.Join(webmanDir, "/pkg")
		webmanBinDir := filepath.Join(webmanDir, "/bin")
		webmanTmpDir := filepath.Join(webmanDir, "/tmp")
		defer os.RemoveAll(webmanTmpDir)
		err = os.MkdirAll(webmanBinDir, os.ModePerm)
		if err != nil {
			panic(err)
		}
		err = os.MkdirAll(webmanPkgDir, os.ModePerm)
		if err != nil {
			panic(err)
		}
		err = os.MkdirAll(webmanTmpDir, os.ModePerm)
		if err != nil {
			panic(err)
		}
		var wg sync.WaitGroup
		ml := multiline.New(len(args), os.Stdout)
		wg.Add(len(args))
		for i, arg := range args {
			go installPkg(arg, i, len(args), webmanDir, &wg, &ml)
		}
		wg.Wait()
	},
}

func init() {
	rootCmd.AddCommand(addCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// addCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// addCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}

func installPkg(arg string, argNum int, argCount int, webmanDir string, wg *sync.WaitGroup, ml *multiline.MultiLogger) {
	defer wg.Done()
	webmanPkgDir := filepath.Join(webmanDir, "/pkg")
	//webmanBinDir := filepath.Join(webmanDir, "/bin")
	webmanTmpDir := filepath.Join(webmanDir, "/tmp")
	parts := strings.Split(arg, "@")
	var pkg string
	var ver string
	var pkgConf pkgparse.PkgConfig
	if len(parts) == 1 {
		pkg = parts[0]
	} else if len(parts) == 2 {
		pkg = parts[0]
		ver = parts[1]
	} else {
		ml.Printf(argNum, "Packages should be in format 'pkg' or 'pkg@version'")
		return
	}
	ml.SetPrefix(argNum, color.YellowString(pkg)+": ")
	// log := log.With().Str("pkg", pkg).Logger()
	pkgConf = pkgparse.ParsePkgConfig(pkg)
	if len(ver) == 0 {
		ml.Printf(argNum, "Finding latest %s version tag", color.CyanString(pkg))
		ver = pkgConf.GetLatestVersion()
		ml.Printf(argNum, "Found %s version tag: %s", color.CyanString(pkg), color.MagentaString(ver))
	}
	stem, ext, url := pkgConf.GetAssetStemExtUrl(ver)
	fileName := stem + "." + ext
	downloadPath := filepath.Join(webmanTmpDir, fileName)

	extractPath := filepath.Join(webmanPkgDir, pkg, stem)
	// If file exists
	if _, err := os.Stat(extractPath); !os.IsNotExist(err) {
		ml.Printf(argNum, "%s@%s is already installed!", color.CyanString(pkg), color.MagentaString(ver))
		return
	}
	ml.Printf(argNum, downloadPath)
	f, err := os.OpenFile(downloadPath,
		os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		ml.Printf(argNum, color.RedString("%v", err))
		return
	}
	defer f.Close()

	r, err := http.Get(url)
	if err != nil {
		ml.Printf(argNum, color.RedString("%v", err))
		return
	}
	defer r.Body.Close()
	bar := progressbar.NewOptions64(r.ContentLength,
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionSetWriter(ioutil.Discard),
		progressbar.OptionShowBytes(true),
		progressbar.OptionFullWidth(),
		progressbar.OptionSetDescription(
			fmt.Sprintf("[cyan][%d/%d][reset] Downloading [cyan]"+pkg+"[reset] file...", argNum+1, argCount)),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "[green]=[reset]",
			SaucerHead:    "[green]>[reset]",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}),
	)
	go func() {
		for !bar.IsFinished() {
			barStr := bar.String()
			ml.Printf(argNum, "%s", barStr)
			time.Sleep(100 * time.Millisecond)
		}
	}()
	_, err = io.Copy(io.MultiWriter(f, bar), r.Body)
	if err != nil {
		ml.Printf(argNum, color.RedString("%v", err))
		return
	}
	hasUnpacked := make(chan bool)
	// This is for threaded printing "..." while unpacking
	go func() {
		i := 0
		for {
			select {
			case <-hasUnpacked:
				return
			default:
				ml.Printf(argNum, "Unpacking %s.%s "+strings.Repeat(".", i), stem, ext)
			}
			time.Sleep(500 * time.Millisecond)
			i += 1
		}
	}()
	err = unpack.Unpack(downloadPath, webmanDir, pkg, stem, ext, pkgConf.ExtractHasRoot)
	hasUnpacked <- true
	if err != nil {
		ml.Printf(argNum, color.RedString("%v", err))
		return
	}
	ml.Printf(argNum, "Completed unpacking %s@%s", color.CyanString(pkg), color.MagentaString(ver))
}
