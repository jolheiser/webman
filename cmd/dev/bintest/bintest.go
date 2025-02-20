package bintest

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"webman/cmd/add"
	"webman/cmd/dev/check"
	"webman/link"
	"webman/multiline"
	"webman/pkgparse"
	"webman/utils"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var OsOptions []string = []string{"windows", "darwin", "linux"}
var ArchOptions []string = []string{"amd64", "arm64"}

// CheckCmd represents the remove command
var BintestCmd = &cobra.Command{
	Use:   "bintest [pkg]",
	Short: "Test the installation & binary paths for each platform for a package",
	Long: `
The "bintest" tests that binary paths given in a package recipe have valid binaries, and displays them.`,
	Example: `webman bintest zoxide -l ~/repos/webman-pkgs/`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) != 1 {
			cmd.Help()
			os.Exit(0)
		}
		utils.Init()
		homedir, err := os.UserHomeDir()
		if err != nil {
			panic(err)
		}
		pkg := args[0]
		var pairResults map[string]bool = map[string]bool{}
		if err := check.CheckPkgConfig(pkg); err != nil {
			color.Red("Pkg Config Error: %v", err)
		}
		pkgConf, err := pkgparse.ParsePkgConfigLocal(pkg, true)
		if err != nil {
			color.Red("Error parsing recipe: %v", err)
			os.Exit(1)
		}
		latestVer, err := pkgConf.GetLatestVersion()
		if err != nil {
			color.Red("Error getting latest version: %v", err)
			os.Exit(1)
		}
		testDir := filepath.Join(homedir, ".webman", "test")
	osLoop:
		for _, osStr := range OsOptions {
			//Example: convert "windows" GOOS to "win" pkgOS
			osPkgStr := pkgparse.GOOStoPkgOs[osStr]
		archLoop:
			for _, arch := range ArchOptions {
				fmt.Println("")
				osPairStr := fmt.Sprintf("%s-%s", osStr, arch)
				if _, osSupported := pkgConf.OsMap[osPkgStr]; !osSupported {
					color.HiBlack("Skipping all %s: unsupported by %s", osStr, pkg)
					continue osLoop
				}
				if _, archSupported := pkgConf.ArchMap[arch]; !archSupported {
					color.HiBlack("Skipping %s-%s: unsupported by %s", osStr, arch, pkg)
					continue archLoop
				}
				for _, pair := range pkgConf.Ignore {
					if pair.Arch == arch && pair.Os == osPkgStr {
						color.HiBlack("Skipping %s-%s: pair ignored by %s", osStr, arch, pkg)
						continue archLoop
					}
				}
				fmt.Printf("Trying %s-%s installation\n", osStr, arch)
				InitTestDir(osStr, arch, homedir, testDir)
				var wg sync.WaitGroup
				ml := multiline.New(len(args), os.Stdout)
				wg.Add(1)
				pairResults[osPairStr] = add.InstallPkg(pkg+"@"+*latestVer, 0, 1, &wg, &ml)

				binPaths, err := pkgConf.GetMyBinPaths()
				if err != nil {
					color.Red("Error getting bin paths: %v", err)
					pairResults[osPairStr] = false
					continue
				}
				binPaths, _, err = link.GetBinPathsAndLinkPaths(pkg, *latestVer, binPaths)
				if err != nil {
					color.Red("Error getting bin paths: %v", err)
					pairResults[osPairStr] = false
					continue
				}
				fmt.Println("  Installation Binary Paths:")
				for i := range binPaths {
					color.Magenta("   %s", binPaths[i])
				}
			}
		}
		allSucceed := true
		fmt.Println("\nResults:")
		for key, val := range pairResults {
			if val {
				color.Green("  %s : SUCCESS", key)
			} else {
				allSucceed = false
				color.Red("  %s : FAIL", key)
			}
		}
		if allSucceed {
			color.HiGreen("\nAll supported OSs & Arches for %s have valid installs!", pkg)
			color.HiBlack("Cleaning up %s", testDir)
			os.RemoveAll(testDir)

		} else {
			color.HiRed("\nSome supported OSs & Arches for %s have invalid installs.", pkg)
			os.Exit(1)
		}
	},
}

func InitTestDir(osStr string, arch string, homedir string, testdir string) {
	utils.WebmanDir = filepath.Join(testdir, osStr, arch)
	utils.WebmanPkgDir = filepath.Join(utils.WebmanDir, "/pkg")
	utils.WebmanBinDir = filepath.Join(utils.WebmanDir, "/bin")
	utils.WebmanTmpDir = filepath.Join(utils.WebmanDir, "/tmp")
	// leave WebmanRecipesDir the way it was

	if err := os.MkdirAll(utils.WebmanBinDir, os.ModePerm); err != nil {
		panic(err)
	}
	if err := os.MkdirAll(utils.WebmanPkgDir, os.ModePerm); err != nil {
		panic(err)
	}
	if err := os.MkdirAll(utils.WebmanTmpDir, os.ModePerm); err != nil {
		panic(err)
	}
	utils.GOOS = osStr
	utils.GOARCH = arch
}

func init() {

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// removeCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// removeCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
