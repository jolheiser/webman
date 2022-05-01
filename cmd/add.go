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
	"webman/link"
	"webman/multiline"
	"webman/pkgparse"
	"webman/unpack"

	"github.com/fatih/color"
	"golang.org/x/sync/errgroup"

	progressbar "github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
)

var doRefresh bool
var recipeDir string

// addCmd represents the add command
var addCmd = &cobra.Command{
	Use:   "add",
	Short: "install packages",
	Long: `
The "add" subcommand installs packages.`,
	Example: `webman add go
webman add go@18.0.0
webman add go zig rg
webman add go@18.0.0 zig@9.1.0 rg@13.0.0`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			cmd.Help()
			os.Exit(0)
		}
		homeDir, err := os.UserHomeDir()
		if err != nil {
			panic(err)
		}
		webmanDir := filepath.Join(homeDir, "/.webman")
		webmanPkgDir := filepath.Join(webmanDir, "/pkg")
		webmanBinDir := filepath.Join(webmanDir, "/bin")
		webmanTmpDir := filepath.Join(webmanDir, "/tmp")
		defer os.RemoveAll(webmanTmpDir)
		if err = os.MkdirAll(webmanBinDir, os.ModePerm); err != nil {
			panic(err)
		}
		if err = os.MkdirAll(webmanPkgDir, os.ModePerm); err != nil {
			panic(err)
		}
		if err = os.MkdirAll(webmanTmpDir, os.ModePerm); err != nil {
			panic(err)
		}
		shouldRefresh, err := pkgparse.ShouldRefreshRecipes(webmanDir)
		if err != nil {
			panic(err)
		}
		if shouldRefresh || doRefresh {
			color.HiBlue("Refreshing package recipes")
			if err = pkgparse.RefreshRecipes(webmanDir); err != nil {
				fmt.Println(err)
			}
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
	addCmd.Flags().BoolVar(&doRefresh, "refresh", false, "force refresh of package recipes")
	addCmd.Flags().StringVarP(&recipeDir, "local-recipes", "l", "", "use given local recipe directory")
	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// addCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// addCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}

func installPkg(arg string, argNum int, argCount int, webmanDir string, wg *sync.WaitGroup, ml *multiline.MultiLogger) bool {
	defer wg.Done()
	webmanPkgDir := filepath.Join(webmanDir, "/pkg")
	webmanTmpDir := filepath.Join(webmanDir, "/tmp")
	parts := strings.Split(arg, "@")
	var pkg string
	var ver string
	if len(parts) == 1 {
		pkg = parts[0]
	} else if len(parts) == 2 {
		pkg = parts[0]
		ver = parts[1]
	} else {
		ml.Printf(argNum, "Packages should be in format 'pkg' or 'pkg@version'")
		return false
	}
	if len(ver) == 0 {
		ml.SetPrefix(argNum, color.YellowString(pkg)+": ")

	} else {
		ml.SetPrefix(argNum, color.YellowString(pkg)+"@"+color.YellowString(ver)+": ")
	}
	foundRecipe := make(chan bool)
	ml.PrintUntilDone(argNum,
		fmt.Sprintf("Finding package recipe for %s", color.CyanString(pkg)),
		foundRecipe,
		500,
	)
	if recipeDir == "" {
		recipeDir = filepath.Join(webmanDir, "recipes")
	}
	pkgConf, err := pkgparse.ParsePkgConfigLocal(recipeDir, pkg)
	foundRecipe <- true
	if err != nil {
		ml.Printf(argNum, color.RedString("%v", err))
		return false
	}
	if len(ver) == 0 {
		foundLatest := make(chan bool)
		ml.PrintUntilDone(argNum,
			fmt.Sprintf("Finding latest %s version tag", color.CyanString(pkg)),
			foundLatest,
			500,
		)
		verPtr, err := pkgConf.GetLatestVersion()
		foundLatest <- true
		if err != nil {
			ml.Printf(argNum, color.RedString("unable to find latest version tag: %v", err))
			return false
		}
		ver = *verPtr
		ml.Printf(argNum, "Found %s version tag: %s", color.CyanString(pkg), color.MagentaString(ver))
	}
	stemPtr, extPtr, urlPtr, err := pkgConf.GetAssetStemExtUrl(ver)
	if err != nil {
		ml.Printf(argNum, color.RedString("%v", err))
		return false
	}
	stem := *stemPtr
	ext := *extPtr
	url := *urlPtr
	fileName := stem + "." + ext
	downloadPath := filepath.Join(webmanTmpDir, fileName)

	extractStem := fmt.Sprintf("%s-%s", pkg, ver)
	extractPath := filepath.Join(webmanPkgDir, pkg, extractStem)
	// If file exists
	if _, err := os.Stat(extractPath); !os.IsNotExist(err) {
		ml.Printf(argNum, color.HiBlackString("Already installed!"))
		return false
	}
	f, err := os.OpenFile(downloadPath,
		os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		ml.Printf(argNum, color.RedString("%v", err))
		return false
	}
	defer f.Close()
	if !DownloadUrl(url, f, pkg, ver, argNum, argCount, ml) {
		return false
	}
	hasUnpacked := make(chan bool)
	ml.PrintUntilDone(argNum,
		fmt.Sprintf("Unpacking %s.%s", stem, ext),
		hasUnpacked,
		500,
	)
	err = unpack.Unpack(downloadPath, webmanDir, pkg, extractStem, ext, pkgConf.ExtractHasRoot)
	hasUnpacked <- true
	if err != nil {
		ml.Printf(argNum, color.RedString("%v", err))
		cleanUpFailedInstall(webmanDir, pkg, extractPath)
		return false
	}
	ml.Printf(argNum, "Completed unpacking %s@%s", color.CyanString(pkg), color.MagentaString(ver))

	using, err := pkgparse.CheckUsing(pkg, webmanDir)
	if err != nil {
		cleanUpFailedInstall(webmanDir, pkg, extractPath)
		panic(err)
	}
	if using == nil {
		binPath, err := pkgConf.GetMyBinPath()
		if err != nil {
			cleanUpFailedInstall(webmanDir, pkg, extractPath)
			ml.Printf(argNum, color.RedString("%v", err))
			return false
		}
		madeLinks, err := CreateLinks(webmanDir, pkg, extractStem, binPath)
		if err != nil {
			cleanUpFailedInstall(webmanDir, pkg, extractPath)
			ml.Printf(argNum, color.RedString("Failed creating links: %v", err))
			return false
		}
		if !madeLinks {
			cleanUpFailedInstall(webmanDir, pkg, extractPath)
			ml.Printf(argNum, color.RedString("Failed creating links"))
			return false
		}
		ml.Printf(argNum, "Now using %s@%s", color.CyanString(pkg), color.MagentaString(ver))
	}
	ml.Printf(argNum, color.GreenString("Successfully installed!"))
	return true
}

func cleanUpFailedInstall(webmanDir string, pkg string, extractPath string) {
	os.RemoveAll(extractPath) // clean up failed installation
	pkgDir := filepath.Join(webmanDir, "pkg", pkg)
	dirs, err := os.ReadDir(pkgDir)
	if err == nil && len(dirs) == 0 {
		os.RemoveAll(pkgDir)
	}
}

func DownloadUrl(url string, f io.Writer, pkg string, ver string, argNum int, argCount int, ml *multiline.MultiLogger) bool {
	r, err := http.Get(url)
	ml.Printf(argNum, "Downloading file at %s", url)
	if err != nil {
		ml.Printf(argNum, color.RedString("%v", err))
		return false
	}
	defer r.Body.Close()
	if !(r.StatusCode >= 200 && r.StatusCode < 300) {
		switch r.StatusCode {
		case 404, 403:
			ml.Printf(argNum, color.RedString("unable to find %s@%s on the web at %s", pkg, ver, url))
		default:
			ml.Printf(argNum, color.RedString("bad HTTP Response: %s", r.Status))
		}
		return false
	}
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
	if _, err = io.Copy(io.MultiWriter(f, bar), r.Body); err != nil {
		ml.Printf(argNum, color.RedString("%v", err))
		return false
	}
	return true
}

func CreateLinks(webmanDir string, pkg string, stem string, confBinPath string) (bool, error) {
	binPaths, linkPaths, err := link.GetBinPathsAndLinkPaths(webmanDir, pkg, stem, confBinPath)
	if err != nil {
		return false, err
	}

	var eg errgroup.Group
	for i, linkPath := range linkPaths {
		binPath := binPaths[i]
		linkPath := linkPath // this supresses the warning for linkPath closure capture
		eg.Go(func() error {
			didLink, err := link.AddLink(binPath, linkPath)
			if err != nil {
				return err
			}
			if !didLink {
				return fmt.Errorf("failed to create link to %s", binPath)
			}
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return false, err
	}
	if err = pkgparse.WriteUsing(pkg, webmanDir, stem); err != nil {
		panic(err)
	}
	return true, nil
}
