// Command schemacheck applies the API description's rules to a descriptor set
// and fails the build if any method breaks one.
//
// It is driven by `make lint`, which produces the descriptor set with
// `buf build`. The rules themselves live in internal/schema and are unit-tested
// there; this is only the part that reads a file and sets an exit code.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/sixfathoms/marque/internal/schema"
)

func main() {
	if err := run(os.Args[1:], os.Stderr); err != nil {
		if _, werr := fmt.Fprintln(os.Stderr, "schemacheck:", err); werr != nil {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: schemacheck <descriptor-set>")
	}

	// The path comes from the Makefile, not from anything a request can reach;
	// this binary is a build tool and is never shipped.
	// #nosec G304,G703 -- the argument is a build-time path under the repository.
	raw, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("reading descriptor set: %w", err)
	}

	// The default resolver is protoregistry.GlobalTypes, which carries the
	// annotation extensions because internal/schema imports the generated
	// package that registers them. Without them the options would decode as
	// unknown fields and every method would look unannotated.
	var fds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(raw, &fds); err != nil {
		return fmt.Errorf("parsing descriptor set: %w", err)
	}

	if len(fds.GetFile()) == 0 {
		return fmt.Errorf("descriptor set %s contains no files; "+
			"a check that inspects nothing is not a check", args[0])
	}

	violations := schema.CheckAnnotations(&fds)
	if len(violations) == 0 {
		return nil
	}

	var report strings.Builder
	for _, v := range violations {
		report.WriteString(v.String())
		report.WriteByte('\n')
	}
	if _, err := io.WriteString(out, report.String()); err != nil {
		return fmt.Errorf("writing report: %w", err)
	}

	return fmt.Errorf("%d method annotation violation(s); see EDR-0020", len(violations))
}
