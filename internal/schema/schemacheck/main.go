// Command schemacheck applies the API description's rules and fails the build
// when one is broken.
//
// It is driven by `make lint` and `make breaking`, which produce the descriptor
// sets with `buf build`. The rules live in internal/schema and are unit-tested
// there; this is the part that reads files, reports, and sets an exit code.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/sixfathoms/marque/internal/schema"
)

type config struct {
	owned     string
	all       string
	before    string
	protoRoot string
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "schemacheck:", err) //nolint:errcheck // nowhere left to report
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	cfg, err := parseFlags(args, stderr)
	if err != nil {
		return err
	}

	owned, err := readDescriptorSet(cfg.owned)
	if err != nil {
		return err
	}
	all, err := readDescriptorSet(cfg.all)
	if err != nil {
		return err
	}

	var violations []schema.Violation

	if cfg.protoRoot != "" {
		onDisk, err := protoFilesUnder(cfg.protoRoot)
		if err != nil {
			return err
		}
		for _, path := range schema.UncoveredProtoFiles(owned, onDisk) {
			violations = append(violations, schema.Violation{
				File:   filepath.Join(cfg.protoRoot, path),
				Method: "(whole file)",
				Reason: "is not in the schema buf builds, so no rule here has ever run on it; " +
					"check buf.yaml's modules and excludes",
			})
		}
	}

	report := schema.CheckAnnotations(owned, all)
	violations = append(violations, report.Violations...)

	// Conditions that make the run itself untrustworthy, as opposed to a
	// violation within it. They are collected rather than returned on sight so
	// that one run reports everything wrong — an operator who fixes the schema
	// serially, one error per build, is an operator who stops reading.
	//
	// Collection covers what the checks find, not what stops them running: an
	// unreadable descriptor set below still returns immediately, discarding
	// anything gathered so far, because there is nothing further to say once an
	// input cannot be read.
	var fatal []string

	// A guard that inspects nothing is the failure worth fearing here: one
	// service file deleted, a buf.yaml `excludes:` entry, or a `--path`
	// narrowing all produce a green run over zero methods.
	if report.MethodsChecked == 0 {
		fatal = append(fatal, fmt.Sprintf("%s declares no methods, so nothing was checked; "+
			"a check that inspects nothing is not a check", cfg.owned))
	}

	compared := ""
	if cfg.before != "" {
		before, err := readDescriptorSet(cfg.before)
		if err != nil {
			return err
		}
		compat := schema.CheckCompatibility(before, owned)
		violations = append(violations, compat.Violations...)

		// Methods are matched by fully-qualified name, so renaming a service —
		// or moving an RPC into another one — matches nothing and compares
		// nothing. `buf breaking` catches a rename and runs first, but a check
		// whose vacuous case is indistinguishable from its passing case is
		// exactly what this command exists not to be.
		if compat.MethodsBefore > 0 && compat.MethodsCompared == 0 {
			fatal = append(fatal, fmt.Sprintf(
				"none of the %d method(s) in %s matched a method here by name, "+
					"so no declaration was compared; a service or method was renamed",
				compat.MethodsBefore, cfg.before))
		}
		// Deleting a method is legal, so a partial match is accepted — it is
		// indistinguishable from a rename under name matching, and buf breaking
		// is what catches the rename. This count is the only signal an operator
		// gets that fewer pairs were compared than existed, which is why it
		// reports what was compared rather than what was available.
		compared = fmt.Sprintf(", %d of %d compared against the previous schema",
			compat.MethodsCompared, compat.MethodsBefore)
	}

	if len(violations) > 0 || len(fatal) > 0 {
		var written strings.Builder
		for _, v := range violations {
			written.WriteString(v.String())
			written.WriteByte('\n')
		}
		for _, f := range fatal {
			written.WriteString(f)
			written.WriteByte('\n')
		}
		if _, err := io.WriteString(stderr, written.String()); err != nil {
			return fmt.Errorf("writing report: %w", err)
		}
		if len(violations) == 0 {
			return fmt.Errorf("%d problem(s) with the schema check itself", len(fatal))
		}
		return fmt.Errorf("%d schema violation(s); see EDR-0020 and EDR-0040", len(violations))
	}

	// Say what was inspected, so a green CI log is evidence rather than silence.
	_, err = fmt.Fprintf(stdout, "schemacheck: %d method(s) checked%s, no violations\n",
		report.MethodsChecked, compared)
	return err
}

func parseFlags(args []string, stderr io.Writer) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("schemacheck", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&cfg.owned, "owned", "", "descriptor set of the files this repository owns (buf build --exclude-imports)")
	fs.StringVar(&cfg.all, "all", "", "descriptor set including imports, for resolving request messages (buf build)")
	fs.StringVar(&cfg.before, "before", "", "optional descriptor set of the previous schema, to check for a weakened declaration")
	fs.StringVar(&cfg.protoRoot, "proto-root", "", "optional directory to check is fully covered by the owned set")

	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if cfg.owned == "" || cfg.all == "" {
		return config{}, errors.New("both -owned and -all are required")
	}
	return cfg, nil
}

func readDescriptorSet(path string) (*descriptorpb.FileDescriptorSet, error) {
	// The path comes from the Makefile, not from anything a request can reach;
	// this binary is a build tool and is never shipped.
	// #nosec G304,G703 -- a build-time path under the repository.
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading descriptor set: %w", err)
	}

	// The default resolver is protoregistry.GlobalTypes, which carries the
	// behaviour extension because internal/schema imports the generated package
	// that registers it. Without it the option would decode as an unknown field
	// and every method would look unannotated — which fails closed, but for the
	// wrong reason.
	//
	// Note that `buf build` without --as-file-descriptor-set emits a buf Image.
	// It is wire-compatible with FileDescriptorSet, which is why this parses;
	// do not "tidy" that flag in without checking what depends on it.
	var fds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(raw, &fds); err != nil {
		return nil, fmt.Errorf("parsing descriptor set %s: %w", path, err)
	}
	if len(fds.GetFile()) == 0 {
		return nil, fmt.Errorf("descriptor set %s contains no files", path)
	}
	return &fds, nil
}

// protoFilesUnder lists every .proto below root, as paths relative to it —
// which is the form buf uses to name files in a descriptor set.
func protoFilesUnder(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".proto" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking %s: %w", root, err)
	}
	return paths, nil
}
