package langserver

import (
	"context"
	"fmt"
	"github.com/saibing/bingo/pkg/lspext"
	"golang.org/x/tools/go/packages/packagestest"
	"log"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/saibing/bingo/langserver/internal/util"

	"github.com/saibing/bingo/pkg/lsp"
	"github.com/sourcegraph/jsonrpc2"
)

func TestDefinition(t *testing.T) {

	exported = packagestest.Export(t, packagestest.Modules, testdata)
	defer exported.Cleanup()

	defer func() {
		if conn != nil {
			if err := conn.Close(); err != nil {
				log.Fatal("conn.Close", err)
			}
		}
	}()

	initServer(exported.Config.Dir)

	test := func(t *testing.T, input string, output string) {
		testDefinition(t, &definitionTestCase{input: input, output: output})
	}

	t.Run("basic definition", func(t *testing.T) {
		test(t, "basic/a.go:1:17", "basic/a.go:1:17-1:18")
		test(t, "basic/a.go:1:23", "basic/a.go:1:17-1:18")
		test(t, "basic/b.go:1:17", "basic/b.go:1:17-1:18")
		test(t, "basic/b.go:1:23", "basic/a.go:1:17-1:18")
	})

	t.Run("subdirectory definition", func(t *testing.T) {
		test(t,  "subdirectory/a.go:1:17", "subdirectory/a.go:1:17-1:18")
		test(t, "subdirectory/a.go:1:23", "subdirectory/a.go:1:17-1:18")
		test(t, "subdirectory/d2/b.go:1:86", "subdirectory/d2/b.go:1:86-1:87")
		test(t, "subdirectory/d2/b.go:1:94", "subdirectory/a.go:1:17-1:18")
		test(t, "subdirectory/d2/b.go:1:99", "subdirectory/d2/b.go:1:86-1:87")
	})

	t.Run("multiple packages in dir", func(t *testing.T) {
		test(t, "multiple/a.go:1:17", "multiple/a.go:1:17-1:18")
		test(t, "multiple/a.go:1:23", "multiple/a.go:1:17-1:18")
	})

	t.Run("go root", func(t *testing.T) {
		test(t, "goroot/a.go:1:40", "goroot/src/fmt/print.go:263:6-263:13")
	})

	t.Run("go project", func(t *testing.T) {
		test(t, "goproject/a/a.go:1:17", "goproject/a/a.go:1:17-1:18")
		test(t, "goproject/b/b.go:1:89", "goproject/a/a.go:1:17-1:18")
	})

	t.Run("go module", func(t *testing.T) {
		test(t, "gomodule/a.go:1:57", "gomodule/d.go:1:19-1:20")
		test(t, "gomodule/b.go:1:63", "gomodule/subp/d.go:1:20-1:21")
		test(t, "gomodule/c.go:1:63", "gomodule/dep1/d1.go:1:58-1:60")
		test(t, "gomodule/c.go:1:68", "gomodule/dep2/d2.go:1:32-1:34")
	})

	t.Run("type definition lookup", func(t *testing.T) {
		test(t, "lookup/b/b.go:1:115", "lookup/b/b.go:1:95-1:96")
	})

	t.Run("go1.9 type alias", func(t *testing.T) {
		test(t, "typealias/a.go:1:17", "typealias/a.go:1:17-1:18")
		test(t, "typealias/b.go:1:17", "typealias/b.go:1:17-1:18")
		test(t, "typealias/b.go:1:20", "")
		test(t, "typealias/b.go:1:21", "typealias/a.go:1:17-1:18")
	})
}

type definitionTestCase struct {
	input  string
	output string
}

func testDefinition(tb testing.TB, c *definitionTestCase) {
	tbRun(tb, fmt.Sprintf("definition-%s", strings.Replace(c.input, "/", "-", -1)), func(t testing.TB) {
		dir, err := filepath.Abs(exported.Config.Dir)
		if err != nil {
			log.Fatal("testDefinition", err)
		}
		doDefinitionTest(t, ctx, conn, util.PathToURI(dir), c.input, c.output, "")
	})
}


func doDefinitionTest(t testing.TB, ctx context.Context, c *jsonrpc2.Conn, rootURI lsp.DocumentURI, pos, want, trimPrefix string) {
	file, line, char, err := parsePos(pos)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := callDefinition(ctx, c, uriJoin(rootURI, file), line, char)
	if err != nil {
		t.Fatal(err)
	}
	if definition != "" {
		definition = util.UriToPath(lsp.DocumentURI(definition))
		if trimPrefix != "" {
			definition = strings.TrimPrefix(definition, util.UriToPath(util.PathToURI(trimPrefix)))
		}
	}
	if want != "" && !strings.Contains(path.Base(want), ":") {
		// our want is just a path, so we only check that matches. This is
		// used by our godef tests into GOROOT. The GOROOT changes over time,
		// but the file for a symbol is usually pretty stable.
		dir := path.Dir(definition)
		base := strings.Split(path.Base(definition), ":")[0]
		definition = path.Join(dir, base)
	}

	want = filepath.Join(exported.Config.Dir, want)
	if definition != want  {
		t.Errorf("got %q, want %q", definition, want)
	}
}

func callDefinition(ctx context.Context, c *jsonrpc2.Conn, uri lsp.DocumentURI, line, char int) (string, error) {
	var res locations
	err := c.Call(ctx, "textDocument/definition", lsp.TextDocumentPositionParams{
		TextDocument: lsp.TextDocumentIdentifier{URI: uri},
		Position:     lsp.Position{Line: line, Character: char},
	}, &res)
	if err != nil {
		return "", err
	}
	var str string
	for i, loc := range res {
		if loc.URI == "" {
			continue
		}
		if i != 0 {
			str += ", "
		}
		str += fmt.Sprintf("%s:%d:%d-%d:%d", loc.URI, loc.Range.Start.Line+1, loc.Range.Start.Character+1, loc.Range.End.Line+1, loc.Range.End.Character+1)
	}
	return str, nil
}

func TestXDefinition(t *testing.T) {

	exported = packagestest.Export(t, packagestest.Modules, testdata)
	defer exported.Cleanup()

	defer func() {
		if conn != nil {
			if err := conn.Close(); err != nil {
				log.Fatal("conn.Close", err)
			}
		}
	}()

	initServer(exported.Config.Dir)

	test := func(t *testing.T, input string, output string) {
		testXDefinition(t, &definitionTestCase{input: input, output: output})
	}

	t.Run("basic definition", func(t *testing.T) {
		test(t, "basic/a.go:1:17", "basic/a.go:1:17 id:github.com/saibing/bingo/langserver/test/pkg/basic/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/basic packageName:p recv: vendor:false")
		test(t, "basic/a.go:1:23", "basic/a.go:1:17 id:github.com/saibing/bingo/langserver/test/pkg/basic/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/basic packageName:p recv: vendor:false")
		test(t, "basic/b.go:1:17", "basic/b.go:1:17 id:github.com/saibing/bingo/langserver/test/pkg/basic/-/B name:B package:github.com/saibing/bingo/langserver/test/pkg/basic packageName:p recv: vendor:false")
		test(t, "basic/b.go:1:23", "basic/a.go:1:17 id:github.com/saibing/bingo/langserver/test/pkg/basic/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/basic packageName:p recv: vendor:false")
	})

	t.Run("subdirectory definition", func(t *testing.T) {
		test(t, "subdirectory/a.go:1:17", "subdirectory/a.go:1:17 id:github.com/saibing/bingo/langserver/test/pkg/subdirectory/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/subdirectory packageName:d recv: vendor:false")
		test(t, "subdirectory/a.go:1:23", "subdirectory/a.go:1:17 id:github.com/saibing/bingo/langserver/test/pkg/subdirectory/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/subdirectory packageName:d recv: vendor:false")
		test(t, "subdirectory/d2/b.go:1:86", "subdirectory/d2/b.go:1:86 id:github.com/saibing/bingo/langserver/test/pkg/subdirectory/d2/-/B name:B package:github.com/saibing/bingo/langserver/test/pkg/subdirectory/d2 packageName:d2 recv: vendor:false")
		test(t, "subdirectory/d2/b.go:1:94", "subdirectory/a.go:1:17 id:github.com/saibing/bingo/langserver/test/pkg/subdirectory/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/subdirectory packageName:d recv: vendor:false")
		test(t, "subdirectory/d2/b.go:1:99", "subdirectory/d2/b.go:1:86 id:github.com/saibing/bingo/langserver/test/pkg/subdirectory/d2/-/B name:B package:github.com/saibing/bingo/langserver/test/pkg/subdirectory/d2 packageName:d2 recv: vendor:false")
	})

	t.Run("multiple packages in dir", func(t *testing.T) {
		test(t, "multiple/a.go:1:17", "multiple/a.go:1:17 id:github.com/saibing/bingo/langserver/test/pkg/multiple/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/multiple packageName:p recv: vendor:false")
		test(t, "multiple/a.go:1:23", "multiple/a.go:1:17 id:github.com/saibing/bingo/langserver/test/pkg/multiple/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/multiple packageName:p recv: vendor:false")
	})

	t.Run("go root", func(t *testing.T) {
		test(t, "goroot/a.go:1:40", "goroot/src/fmt/print.go:263:6 id:fmt/-/Println name:Println package:fmt packageName:fmt recv: vendor:false")
	})

	t.Run("go project", func(t *testing.T) {
		test(t, "goproject/a/a.go:1:17", "goproject/a/a.go:1:17 id:github.com/saibing/bingo/langserver/test/pkg/goproject/a/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/goproject/a packageName:a recv: vendor:false")
		test(t, "goproject/b/b.go:1:89", "goproject/a/a.go:1:17 id:github.com/saibing/bingo/langserver/test/pkg/goproject/a/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/goproject/a packageName:a recv: vendor:false")
	})

	t.Run("go module", func(t *testing.T) {
		test(t, "gomodule/a.go:1:57", "gomodule/d.go:1:19 id:github.com/saibing/dep/-/D name:D package:github.com/saibing/dep packageName:dep recv: vendor:false")
		test(t, "gomodule/b.go:1:63", "gomodule/subp/d.go:1:20 id:github.com/saibing/dep/subp/-/D name:D package:github.com/saibing/dep/subp packageName:subp recv: vendor:false")
		test(t, "gomodule/c.go:1:63", "gomodule/dep1/d1.go:1:58 id:github.com/saibing/dep/dep1/-/D1 name:D1 package:github.com/saibing/dep/dep1 packageName:dep1 recv: vendor:false")
		test(t, "gomodule/c.go:1:68", "gomodule/dep2/d2.go:1:32 id:github.com/saibing/dep/dep2/-/D2/D2 name:D2 package:github.com/saibing/dep/dep2 packageName:dep2 recv:D2 vendor:false")
	})

	t.Run("type definition lookup", func(t *testing.T) {
		test(t, "lookup/b/b.go:1:115", "lookup/b/b.go:1:95 id:github.com/saibing/bingo/langserver/test/pkg/lookup/a/-/A name:A package:github.com/saibing/bingo/langserver/test/pkg/lookup/a packageName:a recv: vendor:false")
	})
}


func testXDefinition(tb testing.TB, c *definitionTestCase) {
	tbRun(tb, fmt.Sprintf("xdefinition-%s", strings.Replace(c.input, "/", "-", -1)), func(t testing.TB) {
		dir, err := filepath.Abs(exported.Config.Dir)
		if err != nil {
			log.Fatal("testXDefinition", err)
		}
		doXDefinitionTest(t, ctx, conn, util.PathToURI(dir), c.input, c.output)
	})
}

func doXDefinitionTest(t testing.TB, ctx context.Context, c *jsonrpc2.Conn, rootURI lsp.DocumentURI, pos, want string) {
	file, line, char, err := parsePos(pos)
	if err != nil {
		t.Fatal(err)
	}
	xdefinition, err := callXDefinition(ctx, c, uriJoin(rootURI, file), line, char)
	if err != nil {
		t.Fatal(err)
	}
	xdefinition = util.UriToPath(lsp.DocumentURI(xdefinition))

	want = filepath.Join(exported.Config.Dir, want)
	if xdefinition != want {
		t.Errorf("\ngot  %q\nwant %q", xdefinition, want)
	}
}

func callXDefinition(ctx context.Context, c *jsonrpc2.Conn, uri lsp.DocumentURI, line, char int) (string, error) {
	var res []lspext.SymbolLocationInformation
	err := c.Call(ctx, "textDocument/xdefinition", lsp.TextDocumentPositionParams{
		TextDocument: lsp.TextDocumentIdentifier{URI: uri},
		Position:     lsp.Position{Line: line, Character: char},
	}, &res)
	if err != nil {
		return "", err
	}
	var str string
	for i, loc := range res {
		if loc.Location.URI == "" {
			continue
		}
		if i != 0 {
			str += ", "
		}
		str += fmt.Sprintf("%s:%d:%d %s", loc.Location.URI, loc.Location.Range.Start.Line+1, loc.Location.Range.Start.Character+1, loc.Symbol)
	}
	return str, nil
}


func TestTypeDefinition(t *testing.T) {
	exported = packagestest.Export(t, packagestest.Modules, testdata)
	defer exported.Cleanup()

	defer func() {
		if conn != nil {
			if err := conn.Close(); err != nil {
				log.Fatal("conn.Close", err)
			}
		}
	}()

	initServer(exported.Config.Dir)

	test := func(t *testing.T, input string, output string) {
		testTypeDefinition(t, &definitionTestCase{input: input, output: output})
	}

	t.Run("type definition lookup", func(t *testing.T) {
		test(t, "lookup/a/a.go:1:58", "lookup/a/a.go:1:17-1:18")
		test(t, "lookup/b/b.go:1:115", "lookup/a/a.go:1:17-1:18")
		test(t, "lookup/c/c.go:1:117", "lookup/a/a.go:1:17-1:18")
		test(t, "lookup/d/d.go:1:135", "")
	})
}

func testTypeDefinition(tb testing.TB, c *definitionTestCase) {
	tbRun(tb, fmt.Sprintf("typeDefinition-%s", strings.Replace(c.input, "/", "-", -1)), func(t testing.TB) {
		dir, err := filepath.Abs(exported.Config.Dir)
		if err != nil {
			log.Fatal("testTypeDefinition", err)
		}
		doTypeDefinitionTest(t, ctx, conn, util.PathToURI(dir), c.input, c.output, "")
	})
}

func doTypeDefinitionTest(t testing.TB, ctx context.Context, c *jsonrpc2.Conn, rootURI lsp.DocumentURI, pos, want, trimPrefix string) {
	file, line, char, err := parsePos(pos)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := callTypeDefinition(ctx, c, uriJoin(rootURI, file), line, char)
	if err != nil {
		t.Fatal(err)
	}
	if definition != "" {
		definition = util.UriToPath(lsp.DocumentURI(definition))
		if trimPrefix != "" {
			definition = strings.TrimPrefix(definition, util.UriToPath(util.PathToURI(trimPrefix)))
		}
	}
	if want != "" && !strings.Contains(path.Base(want), ":") {
		// our want is just a path, so we only check that matches. This is
		// used by our godef tests into GOROOT. The GOROOT changes over time,
		// but the file for a symbol is usually pretty stable.
		dir := path.Dir(definition)
		base := strings.Split(path.Base(definition), ":")[0]
		definition = path.Join(dir, base)
	}

	if want != "" {
		want = filepath.Join(exported.Config.Dir, want)
	}
	if definition != want {
		t.Errorf("got %q, want %q", definition, want)
	}
}


func callTypeDefinition(ctx context.Context, c *jsonrpc2.Conn, uri lsp.DocumentURI, line, char int) (string, error) {
	var res locations
	err := c.Call(ctx, "textDocument/typeDefinition", lsp.TextDocumentPositionParams{
		TextDocument: lsp.TextDocumentIdentifier{URI: uri},
		Position:     lsp.Position{Line: line, Character: char},
	}, &res)
	if err != nil {
		return "", err
	}
	var str string
	for i, loc := range res {
		if loc.URI == "" {
			continue
		}
		if i != 0 {
			str += ", "
		}
		str += fmt.Sprintf("%s:%d:%d-%d:%d", loc.URI, loc.Range.Start.Line+1, loc.Range.Start.Character+1, loc.Range.End.Line+1, loc.Range.End.Character+1)
	}
	return str, nil
}