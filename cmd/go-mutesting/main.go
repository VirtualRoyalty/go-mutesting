package main

import (
	"bytes"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/printer"
	"go/token"
	"go/types"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"gopkg.in/yaml.v3"

	"github.com/VirtualRoyalty/go-mutesting/internal/annotation"
	"github.com/VirtualRoyalty/go-mutesting/internal/console"
	"github.com/VirtualRoyalty/go-mutesting/internal/filter"
	"github.com/VirtualRoyalty/go-mutesting/internal/importing"
	"github.com/VirtualRoyalty/go-mutesting/internal/models"
	"github.com/VirtualRoyalty/go-mutesting/internal/parser"
	"github.com/jessevdk/go-flags"
	"github.com/VirtualRoyalty/osutil"

	"github.com/VirtualRoyalty/go-mutesting"
	"github.com/VirtualRoyalty/go-mutesting/astutil"
	"github.com/VirtualRoyalty/go-mutesting/mutator"
	_ "github.com/VirtualRoyalty/go-mutesting/mutator/arithmetic"
	_ "github.com/VirtualRoyalty/go-mutesting/mutator/branch"
	_ "github.com/VirtualRoyalty/go-mutesting/mutator/expression"
	_ "github.com/VirtualRoyalty/go-mutesting/mutator/loop"
	_ "github.com/VirtualRoyalty/go-mutesting/mutator/numbers"
	_ "github.com/VirtualRoyalty/go-mutesting/mutator/statement"
)

const (
	returnOk = iota
	returnHelp
	returnBashCompletion
	returnError
)

func checkArguments(args []string, opts *models.Options) (bool, int) {
	p := flags.NewNamedParser("go-mutesting", flags.None)

	p.ShortDescription = "Mutation testing for Go source code"

	if _, err := p.AddGroup("go-mutesting", "go-mutesting arguments", opts); err != nil {
		return true, exitError(err.Error())
	}

	completion := len(os.Getenv("GO_FLAGS_COMPLETION")) > 0

	_, err := p.ParseArgs(args)
	if (opts.General.Help || len(args) == 0) && !completion {
		p.WriteHelp(os.Stdout)

		return true, returnHelp
	} else if opts.Mutator.ListMutators {
		for _, name := range mutator.List() {
			fmt.Println(name)
		}

		return true, returnOk
	}

	if err != nil {
		return true, exitError(err.Error())
	}

	if completion {
		return true, returnBashCompletion
	}

	if opts.General.Debug {
		opts.General.Verbose = true
	}

	if opts.General.Config != "" {
		yamlFile, err := os.ReadFile(opts.General.Config)
		if err != nil {
			return true, exitError("Could not read config file: %q", opts.General.Config)
		}
		err = yaml.Unmarshal(yamlFile, &opts.Config)
		if err != nil {
			return true, exitError("Could not unmarshall config file: %q, %v", opts.General.Config, err)
		}
	}

	return false, 0
}

func exitError(format string, args ...interface{}) int {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)

	return returnError
}

type mutatorItem struct {
	Name    string
	Mutator mutator.Mutator
}

func mainCmd(args []string) int {
	var opts = &models.Options{}
	var mutationBlackList = map[string]struct{}{}

	if exit, exitCode := checkArguments(args, opts); exit {
		return exitCode
	}

	files := importing.FilesOfArgs(opts.Remaining.Targets, opts)
	if len(files) == 0 {
		return exitError("Could not find any suitable Go source files")
	}

	if opts.Files.ListFiles {
		for _, file := range files {
			fmt.Println(file)
		}

		return returnOk
	} else if opts.Files.PrintAST {
		for _, file := range files {
			fmt.Println(file)

			src, _, err := parser.ParseFile(file)
			if err != nil {
				return exitError("Could not open file %q: %v", file, err)
			}

			mutesting.PrintWalk(src)

			fmt.Println()
		}

		return returnOk
	}

	if len(opts.Files.Blacklist) > 0 {
		for _, f := range opts.Files.Blacklist {
			c, err := os.ReadFile(f)
			if err != nil {
				return exitError("Cannot read blacklist file %q: %v", f, err)
			}

			for _, line := range strings.Split(string(c), "\n") {
				if line == "" {
					continue
				}

				if len(line) != 32 {
					return exitError("%q is not a MD5 checksum", line)
				}

				mutationBlackList[line] = struct{}{}
			}
		}
	}

	var mutators []mutatorItem

MUTATOR:
	for _, name := range mutator.List() {
		if len(opts.Mutator.DisableMutators) > 0 {
			for _, d := range opts.Mutator.DisableMutators {
				pattern := strings.HasSuffix(d, "*")

				if (pattern && strings.HasPrefix(name, d[:len(d)-2])) || (!pattern && name == d) {
					continue MUTATOR
				}
			}
		}

		console.Verbose(opts, "Enable mutator %q", name)

		m, _ := mutator.New(name)
		mutators = append(mutators, mutatorItem{
			Name:    name,
			Mutator: m,
		})
	}

	tmpDir, err := os.MkdirTemp("", "go-mutesting-")
	if err != nil {
		panic(err)
	}
	console.Verbose(opts, "Save mutations into %q", tmpDir)

	var execs []string
	if opts.Exec.Exec != "" {
		execs = strings.Split(opts.Exec.Exec, " ")
	}

	report := &models.Report{}

	for _, file := range files {
		console.Verbose(opts, "Mutate %q", file)

		annotationProcessor := annotation.NewProcessor()
		skipFilterProcessor := filter.NewSkipMakeArgsFilter()

		collectors := []filter.NodeCollector{
			annotationProcessor,
			skipFilterProcessor,
		}

		filters := []filter.NodeFilter{
			annotationProcessor,
			skipFilterProcessor,
		}

		src, fset, pkg, info, err := parser.ParseAndTypeCheckFile(file, collectors)
		if err != nil {
			return exitError(err.Error())
		}

		err = os.MkdirAll(tmpDir+"/"+filepath.Dir(file), 0755)
		if err != nil {
			panic(err)
		}

		tmpFile := tmpDir + "/" + file

		originalFile := fmt.Sprintf("%s.original", tmpFile)
		err = osutil.CopyFile(file, originalFile)
		if err != nil {
			panic(err)
		}
		console.Debug(opts, "Save original into %q", originalFile)

		mutationID := 0

		if opts.Filter.Match != "" {
			m, err := regexp.Compile(opts.Filter.Match)
			if err != nil {
				return exitError("Match regex is not valid: %v", err)
			}

			for _, f := range astutil.Functions(src) {
				if m.MatchString(f.Name.Name) {
					mutationID = mutate(opts, mutators, mutationBlackList, mutationID, pkg, info, file, fset, src, f, tmpFile, execs, report, filters)
				}
			}
		} else {
			_ = mutate(opts, mutators, mutationBlackList, mutationID, pkg, info, file, fset, src, src, tmpFile, execs, report, filters)
		}
	}

	if !opts.General.DoNotRemoveTmpFolder {
		err = os.RemoveAll(tmpDir)
		if err != nil {
			panic(err)
		}
		console.Debug(opts, "Remove %q", tmpDir)
	}

	report.Calculate()

	if !opts.Exec.NoExec {
		if !opts.Config.SilentMode {
			fmt.Printf("The mutation score is %f (%d passed, %d failed, %d duplicated, %d skipped, total is %d)\n",
				report.Stats.Msi,
				report.Stats.KilledCount,
				report.Stats.EscapedCount,
				report.Stats.DuplicatedCount,
				report.Stats.SkippedCount,
				report.Stats.TotalMutantsCount,
			)
		}
	} else {
		fmt.Println("Cannot do a mutation testing summary since no exec command was executed.")
	}

	jsonContent, err := json.Marshal(report)
	if err != nil {
		return exitError(err.Error())
	}

	file, err := os.OpenFile(models.ReportFileName, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		return exitError(err.Error())
	}

	if file == nil {
		return exitError("Cannot create file for report")
	}

	defer func() {
		err = file.Close()
		if err != nil {
			fmt.Printf("Error while report file closing: %v", err.Error())
		}
	}()

	_, err = file.WriteString(string(jsonContent))
	if err != nil {
		return exitError(err.Error())
	}

	console.Verbose(opts, "Save report into %q", models.ReportFileName)

	return returnOk
}

func mutate(
	opts *models.Options,
	mutators []mutatorItem,
	mutationBlackList map[string]struct{},
	mutationID int,
	pkg *types.Package,
	info *types.Info,
	originalFile string,
	fset *token.FileSet,
	src ast.Node,
	node ast.Node,
	mutatedFile string,
	execs []string,
	stats *models.Report,
	filters []filter.NodeFilter,
) int {
	for _, m := range mutators {
		console.Debug(opts, "Mutator %s", m.Name)

		mutatorAnnotated := annotation.DecoratorFilter(m.Mutator, m.Name, filters...)

		changed := mutesting.MutateWalk(pkg, info, node, mutatorAnnotated)

		for {
			_, ok := <-changed

			if !ok {
				break
			}

			originalSourceCode, err := os.ReadFile(originalFile)
			if err != nil {
				log.Fatal(err)
			}

			mutant := models.Mutant{}
			mutant.Mutator.MutatorName = m.Name
			mutant.Mutator.OriginalFilePath = originalFile
			mutant.Mutator.OriginalSourceCode = string(originalSourceCode)

			mutationFile := fmt.Sprintf("%s.%d", mutatedFile, mutationID)
			checksum, duplicate, err := saveAST(mutationBlackList, mutationFile, fset, src)
			if err != nil {
				fmt.Printf("INTERNAL ERROR %s\n", err.Error())
			} else if duplicate {
				console.Debug(opts, "%q is a duplicate, we ignore it", mutationFile)

				stats.Stats.DuplicatedCount++
			} else {
				console.Debug(opts, "Save mutation into %q with checksum %s", mutationFile, checksum)

				if !opts.Exec.NoExec {
					execExitCode := mutateExec(opts, pkg, originalFile, src, mutationFile, execs, &mutant)

					console.Debug(opts, "Exited with %d", execExitCode)

					mutatedSourceCode, err := os.ReadFile(mutationFile)
					if err != nil {
						log.Fatal(err)
					}
					mutant.Mutator.MutatedSourceCode = string(mutatedSourceCode)

					msg := fmt.Sprintf("%q with checksum %s", mutationFile, checksum)

					switch execExitCode {
					case 0: // Tests failed - all ok
						out := fmt.Sprintf("PASS %s\n", msg)
						if !opts.Config.SilentMode {
							console.PrintPass(out)
						}

						mutant.ProcessOutput = out
						stats.Killed = append(stats.Killed, mutant)
						stats.Stats.KilledCount++
					case 1: // Tests passed
						out := fmt.Sprintf("FAIL %s\n", msg)
						if !opts.Config.SilentMode {
							console.PrintFail(out)
						}

						mutant.ProcessOutput = out
						stats.Escaped = append(stats.Escaped, mutant)
						stats.Stats.EscapedCount++
					case 2: // Did not compile
						out := fmt.Sprintf("SKIP %s\n", msg)
						if !opts.Config.SilentMode {
							console.PrintSkip(out)
						}

						mutant.ProcessOutput = out
						stats.Stats.SkippedCount++
					default:
						out := fmt.Sprintf("UNKOWN exit code for %s\n", msg)
						if !opts.Config.SilentMode {
							console.PrintUnknown(out)
						}

						mutant.ProcessOutput = out
						stats.Errored = append(stats.Errored, mutant)
						stats.Stats.ErrorCount++
					}
				}
			}

			changed <- true

			// Ignore original state
			<-changed
			changed <- true

			mutationID++
		}
	}

	return mutationID
}

func mutateExec(
	opts *models.Options,
	pkg *types.Package,
	file string,
	src ast.Node,
	mutationFile string,
	execs []string,
	mutant *models.Mutant,
) (execExitCode int) {
	if len(execs) == 0 {
		console.Debug(opts, "Execute built-in exec command for mutation")

		diff, err := exec.Command("diff", "--label=Original", "--label=New", "-u", file, mutationFile).CombinedOutput()

		startLine := parser.FindOriginalStartLine(diff)
		mutant.Mutator.OriginalStartLine = startLine

		if err == nil {
			execExitCode = 0
		} else if e, ok := err.(*exec.ExitError); ok {
			execExitCode = e.Sys().(syscall.WaitStatus).ExitStatus()
		} else {
			panic(err)
		}
		if execExitCode != 0 && execExitCode != 1 {
			fmt.Printf("%s\n", diff)

			panic("Could not execute diff on mutation file")
		}

		defer func() {
			_ = os.Rename(file+".tmp", file)
		}()

		err = os.Rename(file, file+".tmp")
		if err != nil {
			panic(err)
		}
		err = osutil.CopyFile(mutationFile, file)
		if err != nil {
			panic(err)
		}

		pkgName := pkg.Path()
		if opts.Test.Recursive {
			pkgName += "/..."
		}

		goTestCmd := exec.Command("go", "test", "-timeout", fmt.Sprintf("%ds", opts.Exec.Timeout), pkgName)
		goTestCmd.Env = os.Environ()

		test, err := goTestCmd.CombinedOutput()
		if err == nil {
			execExitCode = 0
		} else if e, ok := err.(*exec.ExitError); ok {
			execExitCode = e.Sys().(syscall.WaitStatus).ExitStatus()
		} else {
			panic(err)
		}

		if opts.General.Debug {
			fmt.Printf("%s\n", test)
		}

		mutant.Diff = string(diff)

		switch execExitCode {
		case 0: // Tests passed -> FAIL
			if !opts.Config.SilentMode {
				console.PrintDiff(diff)
			}

			execExitCode = 1
		case 1: // Tests failed -> PASS
			if opts.General.Debug {
				console.PrintDiff(diff)
			}

			execExitCode = 0
		case 2: // Did not compile -> SKIP
			if opts.General.Verbose {
				fmt.Println("Mutation did not compile")
			}

			if opts.General.Debug {
				console.PrintDiff(diff)
			}
		default: // Unknown exit code -> SKIP
			if !opts.Config.SilentMode {
				fmt.Println("Unknown exit code")
				console.PrintDiff(diff)
			}
		}

		return execExitCode
	}

	console.Debug(opts, "Execute %q for mutation", opts.Exec.Exec)

	execCommand := exec.Command(execs[0], execs[1:]...)

	execCommand.Stderr = os.Stderr
	execCommand.Stdout = os.Stdout

	execCommand.Env = append(os.Environ(), []string{
		"MUTATE_CHANGED=" + mutationFile,
		fmt.Sprintf("MUTATE_DEBUG=%t", opts.General.Debug),
		"MUTATE_ORIGINAL=" + file,
		"MUTATE_PACKAGE=" + pkg.Path(),
		fmt.Sprintf("MUTATE_TIMEOUT=%d", opts.Exec.Timeout),
		fmt.Sprintf("MUTATE_VERBOSE=%t", opts.General.Verbose),
	}...)
	if opts.Test.Recursive {
		execCommand.Env = append(execCommand.Env, "TEST_RECURSIVE=true")
	}

	err := execCommand.Start()
	if err != nil {
		panic(err)
	}

	// TODO timeout here

	err = execCommand.Wait()

	if err == nil {
		execExitCode = 0
	} else if e, ok := err.(*exec.ExitError); ok {
		execExitCode = e.Sys().(syscall.WaitStatus).ExitStatus()
	} else {
		panic(err)
	}

	return execExitCode
}

func main() {
	os.Exit(mainCmd(os.Args[1:]))
}

func saveAST(mutationBlackList map[string]struct{}, file string, fset *token.FileSet, node ast.Node) (string, bool, error) {
	var buf bytes.Buffer

	h := md5.New()

	err := printer.Fprint(io.MultiWriter(h, &buf), fset, node)
	if err != nil {
		return "", false, err
	}

	checksum := fmt.Sprintf("%x", h.Sum(nil))

	if _, ok := mutationBlackList[checksum]; ok {
		return checksum, true, nil
	}

	mutationBlackList[checksum] = struct{}{}

	src, err := format.Source(buf.Bytes())
	if err != nil {
		return "", false, err
	}

	err = os.WriteFile(file, src, 0666)
	if err != nil {
		return "", false, err
	}

	return checksum, false, nil
}
