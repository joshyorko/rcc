package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

const schemaVersion = 1

type position struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

type reference struct {
	CallerPackage string   `json:"caller_package"`
	CallerName    string   `json:"caller_name"`
	Position      position `json:"position"`
	Kind          string   `json:"kind"`
	Test          bool     `json:"test"`
}

type call struct {
	CalleePackage  string   `json:"callee_package"`
	CalleeName     string   `json:"callee_name"`
	Position       position `json:"position"`
	Kind           string   `json:"kind"`
	Test           bool     `json:"test"`
	CalleePosition position `json:"callee_position"`
}

type symbol struct {
	Package              string      `json:"package"`
	Name                 string      `json:"name"`
	Position             position    `json:"position"`
	Exported             bool        `json:"exported"`
	Generated            bool        `json:"generated"`
	TestFile             bool        `json:"test_file"`
	Marker               bool        `json:"marker"`
	Linkname             bool        `json:"linkname"`
	InterfaceObligations []string    `json:"interface_obligations"`
	References           []reference `json:"references"`
	Calls                []call      `json:"calls"`
}

type inventory struct {
	SchemaVersion int      `json:"schema_version"`
	Symbols       []symbol `json:"symbols"`
}

type stringList []string

func (values *stringList) String() string {
	return strings.Join(*values, ",")
}

func (values *stringList) Set(value string) error {
	*values = append(*values, value)
	return nil
}

type options struct {
	root    string
	tags    string
	symbols []string
}

type symbolID struct {
	packagePath string
	name        string
}

type declaration struct {
	key       string
	id        symbolID
	function  *types.Func
	node      *ast.FuncDecl
	pkg       *packages.Package
	position  position
	generated bool
	testFile  bool
	linkname  bool
	root      bool
	inRoot    bool
}

type interfaceMethod struct {
	name        string
	packagePath string
	iface       *types.Interface
	method      *types.Func
}

type builder struct {
	root               string
	fset               *token.FileSet
	declarations       []*declaration
	declarationByKey   map[string]*declaration
	interfacesByMethod map[string][]interfaceMethod
}

type referenceKey struct {
	caller   string
	position position
	kind     string
	test     bool
}

type callKey struct {
	callee   string
	position position
	kind     string
	test     bool
}

type functionUse struct {
	function *types.Func
	position token.Pos
	kind     string
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "deadcodeinventory: %v\n", err)
		os.Exit(1)
	}
}

func run(arguments []string, stdout, stderr io.Writer) error {
	options, patterns, err := parseFlags(arguments, stderr)
	if err != nil {
		return err
	}

	root, err := filepath.Abs(options.root)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	root = filepath.Clean(root)

	fset := token.NewFileSet()
	config := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedCompiledGoFiles |
			packages.NeedImports |
			packages.NeedDeps |
			packages.NeedTypes |
			packages.NeedSyntax |
			packages.NeedTypesInfo,
		Dir:   root,
		Fset:  fset,
		Tests: true,
		ParseFile: func(fset *token.FileSet, filename string, src []byte) (*ast.File, error) {
			return parser.ParseFile(fset, filename, src, parser.ParseComments)
		},
	}
	if options.tags != "" {
		config.BuildFlags = []string{"-tags=" + options.tags}
	}

	pkgs, err := packages.Load(config, patterns...)
	if err != nil {
		return fmt.Errorf("load packages: %w", err)
	}
	if count := writePackageErrors(pkgs, stderr); count != 0 {
		return fmt.Errorf("package loading failed with %d error(s)", count)
	}

	selected, err := parseSelectedSymbols(options.symbols)
	if err != nil {
		return err
	}
	report, err := buildInventory(root, fset, pkgs, selected)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(stdout).Encode(report); err != nil {
		return fmt.Errorf("encode inventory: %w", err)
	}
	return nil
}

func parseFlags(arguments []string, stderr io.Writer) (options, []string, error) {
	flags := flag.NewFlagSet("deadcodeinventory", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var symbols stringList
	root := flags.String("root", ".", "repository root used for loading and relative positions")
	tags := flags.String("tags", "", "comma-separated Go build tags")
	flags.Var(&symbols, "symbol", "function to report as full/import/path|FuncOrType.Method (repeatable)")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: deadcodeinventory -root /repo -tags '' -symbol 'full/import/path|FuncOrType.Method' -- ./package/pattern [...]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(arguments); err != nil {
		return options{}, nil, err
	}
	if flags.NArg() == 0 {
		flags.Usage()
		return options{}, nil, fmt.Errorf("at least one package pattern is required")
	}
	return options{root: *root, tags: *tags, symbols: symbols}, flags.Args(), nil
}

func parseSelectedSymbols(specifications []string) (map[symbolID]bool, error) {
	selected := make(map[symbolID]bool, len(specifications))
	for _, specification := range specifications {
		packagePath, name, found := strings.Cut(specification, "|")
		if !found || packagePath == "" || name == "" {
			return nil, fmt.Errorf("invalid symbol %q: want full/import/path|FuncOrType.Method", specification)
		}
		selected[symbolID{packagePath: packagePath, name: name}] = true
	}
	return selected, nil
}

func writePackageErrors(pkgs []*packages.Package, stderr io.Writer) int {
	messages := make(map[string]bool)
	packages.Visit(pkgs, nil, func(pkg *packages.Package) {
		for _, err := range pkg.Errors {
			messages[err.Error()] = true
		}
	})
	ordered := make([]string, 0, len(messages))
	for message := range messages {
		ordered = append(ordered, message)
	}
	sort.Strings(ordered)
	for _, message := range ordered {
		fmt.Fprintln(stderr, message)
	}
	return len(ordered)
}

func buildInventory(root string, fset *token.FileSet, pkgs []*packages.Package, selected map[symbolID]bool) (inventory, error) {
	roots := make(map[string]bool, len(pkgs))
	for _, pkg := range pkgs {
		roots[pkg.ID] = true
	}
	builder := &builder{
		root:               root,
		fset:               fset,
		declarationByKey:   make(map[string]*declaration),
		interfacesByMethod: make(map[string][]interfaceMethod),
	}
	packages.Visit(pkgs, func(pkg *packages.Package) bool {
		builder.collectInterfaces(pkg)
		builder.collectDeclarations(pkg, roots[pkg.ID])
		return true
	}, nil)
	builder.collectUniverseInterfaces()

	chosen := make([]*declaration, 0)
	for _, decl := range builder.declarations {
		if len(selected) == 0 {
			if decl.root && decl.inRoot {
				chosen = append(chosen, decl)
			}
			continue
		}
		if selected[decl.id] {
			chosen = append(chosen, decl)
		}
	}
	sort.Slice(chosen, func(i, j int) bool {
		return declarationLess(chosen[i], chosen[j])
	})

	report := inventory{SchemaVersion: schemaVersion, Symbols: make([]symbol, len(chosen))}
	indices := make(map[string]int, len(chosen))
	for index, decl := range chosen {
		indices[decl.key] = index
		obligations := builder.interfaceObligations(decl)
		marker := decl.node.Body != nil && len(decl.node.Body.List) == 0 && len(obligations) != 0
		report.Symbols[index] = symbol{
			Package:              decl.id.packagePath,
			Name:                 decl.id.name,
			Position:             decl.position,
			Exported:             decl.function.Exported(),
			Generated:            decl.generated,
			TestFile:             decl.testFile,
			Marker:               marker,
			Linkname:             decl.linkname,
			InterfaceObligations: obligations,
			References:           make([]reference, 0),
			Calls:                make([]call, 0),
		}
	}

	references := make([]map[referenceKey]reference, len(chosen))
	calls := make([]map[callKey]call, len(chosen))
	for index := range chosen {
		references[index] = make(map[referenceKey]reference)
		calls[index] = make(map[callKey]call)
	}
	for _, caller := range builder.declarations {
		if caller.node.Body == nil {
			continue
		}
		for _, use := range functionUses(caller) {
			calleeID, ok := functionID(use.function)
			if !ok {
				continue
			}
			calleeKey := builder.functionKey(use.function)
			usePosition := builder.position(use.position)
			isTest := strings.HasSuffix(usePosition.File, "_test.go")

			if index, found := indices[calleeKey]; found {
				key := referenceKey{caller: caller.key, position: usePosition, kind: use.kind, test: isTest}
				references[index][key] = reference{
					CallerPackage: caller.id.packagePath,
					CallerName:    caller.id.name,
					Position:      usePosition,
					Kind:          use.kind,
					Test:          isTest,
				}
			}
			if index, found := indices[caller.key]; found {
				key := callKey{callee: calleeKey, position: usePosition, kind: use.kind, test: isTest}
				calls[index][key] = call{
					CalleePackage:  calleeID.packagePath,
					CalleeName:     calleeID.name,
					Position:       usePosition,
					Kind:           use.kind,
					Test:           isTest,
					CalleePosition: builder.position(use.function.Pos()),
				}
			}
		}
	}

	for index := range report.Symbols {
		for _, ref := range references[index] {
			report.Symbols[index].References = append(report.Symbols[index].References, ref)
		}
		sort.Slice(report.Symbols[index].References, func(i, j int) bool {
			return referenceLess(report.Symbols[index].References[i], report.Symbols[index].References[j])
		})
		for _, call := range calls[index] {
			report.Symbols[index].Calls = append(report.Symbols[index].Calls, call)
		}
		sort.Slice(report.Symbols[index].Calls, func(i, j int) bool {
			return callLess(report.Symbols[index].Calls[i], report.Symbols[index].Calls[j])
		})
	}
	return report, nil
}

func (builder *builder) collectDeclarations(pkg *packages.Package, root bool) {
	if pkg.TypesInfo == nil {
		return
	}
	for _, file := range pkg.Syntax {
		generated := ast.IsGenerated(file)
		for _, fileDecl := range file.Decls {
			node, ok := fileDecl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			function, ok := pkg.TypesInfo.Defs[node.Name].(*types.Func)
			if !ok {
				continue
			}
			id, ok := functionID(function)
			if !ok {
				continue
			}
			key := builder.functionKey(function)
			if existing, found := builder.declarationByKey[key]; found {
				existing.root = existing.root || root
				continue
			}
			pos := builder.position(function.Pos())
			decl := &declaration{
				key:       key,
				id:        id,
				function:  function,
				node:      node,
				pkg:       pkg,
				position:  pos,
				generated: generated,
				testFile:  strings.HasSuffix(pos.File, "_test.go"),
				linkname:  hasLinkname(node.Doc),
				root:      root,
				inRoot:    positionInRoot(pos),
			}
			builder.declarationByKey[key] = decl
			builder.declarations = append(builder.declarations, decl)
		}
	}
}

func (builder *builder) collectInterfaces(pkg *packages.Package) {
	if pkg.Types == nil {
		return
	}
	for _, name := range pkg.Types.Scope().Names() {
		typeName, ok := pkg.Types.Scope().Lookup(name).(*types.TypeName)
		if ok {
			builder.collectInterface(typeName)
		}
	}
}

func (builder *builder) collectUniverseInterfaces() {
	typeName, ok := types.Universe.Lookup("error").(*types.TypeName)
	if ok {
		builder.collectInterface(typeName)
	}
}

func (builder *builder) collectInterface(typeName *types.TypeName) {
	named, ok := typeName.Type().(*types.Named)
	if !ok {
		return
	}
	iface, ok := named.Underlying().(*types.Interface)
	if !ok {
		return
	}
	iface.Complete()
	packagePath := ""
	if typeName.Pkg() != nil {
		packagePath = typeName.Pkg().Path()
	}
	for index := 0; index < iface.NumMethods(); index++ {
		method := iface.Method(index)
		builder.interfacesByMethod[method.Name()] = append(builder.interfacesByMethod[method.Name()], interfaceMethod{
			name:        typeName.Name(),
			packagePath: packagePath,
			iface:       iface,
			method:      method,
		})
	}
}

func (builder *builder) interfaceObligations(decl *declaration) []string {
	signature, ok := decl.function.Type().(*types.Signature)
	if !ok || signature.Recv() == nil {
		return []string{}
	}
	set := make(map[string]bool)
	for _, candidate := range builder.interfacesByMethod[decl.function.Name()] {
		if !types.Implements(signature.Recv().Type(), candidate.iface) {
			continue
		}
		if !types.Identical(decl.function.Type(), candidate.method.Type()) {
			continue
		}
		name := candidate.name + "." + candidate.method.Name()
		if candidate.packagePath != "" {
			name = candidate.packagePath + "." + name
		}
		set[name] = true
	}
	obligations := make([]string, 0, len(set))
	for obligation := range set {
		obligations = append(obligations, obligation)
	}
	sort.Strings(obligations)
	return obligations
}

func functionUses(decl *declaration) []functionUse {
	called := make(map[*ast.Ident]bool)
	ast.Inspect(decl.node.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if ok {
			if identifier := calledIdentifier(call.Fun); identifier != nil {
				called[identifier] = true
			}
		}
		return true
	})

	uses := make([]functionUse, 0)
	ast.Inspect(decl.node.Body, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if !ok {
			return true
		}
		function, ok := decl.pkg.TypesInfo.Uses[identifier].(*types.Func)
		if !ok {
			return true
		}
		kind := "reference"
		if called[identifier] {
			kind = "call"
		}
		uses = append(uses, functionUse{function: function, position: identifier.Pos(), kind: kind})
		return true
	})
	return uses
}

func calledIdentifier(expression ast.Expr) *ast.Ident {
	for {
		switch expr := expression.(type) {
		case *ast.ParenExpr:
			expression = expr.X
		case *ast.IndexExpr:
			expression = expr.X
		case *ast.IndexListExpr:
			expression = expr.X
		case *ast.Ident:
			return expr
		case *ast.SelectorExpr:
			return expr.Sel
		default:
			return nil
		}
	}
}

func functionID(function *types.Func) (symbolID, bool) {
	if function == nil || function.Pkg() == nil {
		return symbolID{}, false
	}
	name := function.Name()
	signature, ok := function.Type().(*types.Signature)
	if ok && signature.Recv() != nil {
		receiver := signature.Recv().Type()
		if pointer, ok := receiver.(*types.Pointer); ok {
			receiver = pointer.Elem()
		}
		if named, ok := receiver.(*types.Named); ok {
			name = named.Obj().Name() + "." + name
		}
	}
	return symbolID{packagePath: function.Pkg().Path(), name: name}, true
}

func (builder *builder) functionKey(function *types.Func) string {
	id, _ := functionID(function)
	pos := builder.position(function.Pos())
	return fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%d", id.packagePath, id.name, pos.File, pos.Line, pos.Column)
}

func (builder *builder) position(pos token.Pos) position {
	if !pos.IsValid() {
		return position{}
	}
	tokenPosition := builder.fset.Position(pos)
	filename := tokenPosition.Filename
	if filename != "" {
		if relative, err := filepath.Rel(builder.root, filename); err == nil {
			filename = relative
		}
	}
	return position{File: filepath.ToSlash(filename), Line: tokenPosition.Line, Column: tokenPosition.Column}
}

func positionInRoot(pos position) bool {
	if pos.File == "" || filepath.IsAbs(pos.File) {
		return false
	}
	return pos.File != ".." && !strings.HasPrefix(pos.File, "../")
}

func hasLinkname(group *ast.CommentGroup) bool {
	if group == nil {
		return false
	}
	for _, comment := range group.List {
		if !strings.HasPrefix(comment.Text, "//go:linkname") {
			continue
		}
		rest := strings.TrimPrefix(comment.Text, "//go:linkname")
		if rest == "" || rest[0] == ' ' || rest[0] == '\t' {
			return true
		}
	}
	return false
}

func declarationLess(left, right *declaration) bool {
	if left.id.packagePath != right.id.packagePath {
		return left.id.packagePath < right.id.packagePath
	}
	if left.id.name != right.id.name {
		return left.id.name < right.id.name
	}
	return positionLess(left.position, right.position)
}

func referenceLess(left, right reference) bool {
	if left.CallerPackage != right.CallerPackage {
		return left.CallerPackage < right.CallerPackage
	}
	if left.CallerName != right.CallerName {
		return left.CallerName < right.CallerName
	}
	if left.Position != right.Position {
		return positionLess(left.Position, right.Position)
	}
	if left.Kind != right.Kind {
		return left.Kind < right.Kind
	}
	return !left.Test && right.Test
}

func callLess(left, right call) bool {
	if left.CalleePackage != right.CalleePackage {
		return left.CalleePackage < right.CalleePackage
	}
	if left.CalleeName != right.CalleeName {
		return left.CalleeName < right.CalleeName
	}
	if left.Position != right.Position {
		return positionLess(left.Position, right.Position)
	}
	if left.Kind != right.Kind {
		return left.Kind < right.Kind
	}
	return !left.Test && right.Test
}

func positionLess(left, right position) bool {
	if left.File != right.File {
		return left.File < right.File
	}
	if left.Line != right.Line {
		return left.Line < right.Line
	}
	return left.Column < right.Column
}
