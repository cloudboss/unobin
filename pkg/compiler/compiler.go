// Copyright © 2020 Joseph Wright <joseph@cloudboss.co>
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

//go:generate peg grammar.peg

package compiler

import (
	"fmt"
	"go/ast"
	"go/token"
	"io/ioutil"
	"strconv"

	"github.com/cloudboss/unobin/pkg/module"
	"github.com/cloudboss/unobin/pkg/util"
	"github.com/hashicorp/go-multierror"
	"github.com/xeipuuv/gojsonschema"
)

type Compiler struct {
	file          string
	moduleImports map[string]*module.ModuleImport
	grammar       *Grammar
}

func NewCompiler(file string) *Compiler {
	return &Compiler{
		file:          file,
		moduleImports: map[string]*module.ModuleImport{},
	}
}

// Load the compiler's playbook file and parse it into the Unobin AST or return an error.
func (c *Compiler) Load() error {
	b, err := ioutil.ReadFile(c.file)
	if err != nil {
		return err
	}
	grammar := &Grammar{Buffer: string(b)}
	err = grammar.Init()
	if err != nil {
		return err
	}
	err = grammar.Parse()
	if err != nil {
		return err
	}
	grammar.LoadUAST()
	c.grammar = grammar
	return nil
}

// Validate checks for errors not caught by the parser. For example, the parser only knows
// that attributes should be pairs of string -> value, but does not enforce the underlying
// type required for the value. This function ensures that:
// * Required attributes are present: name, description, imports, and input-schema, and
//   that they are the correct type.
// * Imports are defined as an object with string keys and string values, and that the
//   import paths are correctly formatted. TODO: check that module exists on the given path.
// * The input schema is a valid JSON schema.
// * Block tasks refer only to modules that are defined in imports.
func (c *Compiler) Validate() error {
	var err error
	attrErr := c.validateAttributes(c.grammar.uast.Attributes)
	if attrErr != nil {
		err = multierror.Append(err, attrErr)
	}
	imports := c.grammar.uast.Attributes[importsAttr]
	if imports != nil && imports.Object != nil {
		importsErr := c.validateImports(c.grammar.uast.Attributes[importsAttr])
		if importsErr != nil {
			err = multierror.Append(err, importsErr)
		}
	}
	schemaErr := c.validateInputSchema(c.grammar.uast.Attributes[inputSchemaAttr])
	if schemaErr != nil {
		err = multierror.Append(err, schemaErr)
	}
	blockErr := c.validateBlocks(c.grammar.uast.Blocks)
	if blockErr != nil {
		err = multierror.Append(err, blockErr)
	}
	return err
}

// validateAttributes ensures that playbook attributes are defined with the correct types.
// This checks only the high level types, for example that imports is an Object. This is
// checked further by validateImports later to ensure that the Object values are strings.
func (c *Compiler) validateAttributes(attributes ObjectExpr) error {
	validAttributes := map[string]Type{
		nameAttr:        StringType,
		descriptionAttr: StringType,
		importsAttr:     ObjectType,
		inputSchemaAttr: ObjectType,
	}
	var err error
	for k, v := range attributes {
		validType, found := validAttributes[k]
		if !found {
			err = multierror.Append(err, fmt.Errorf("unknown attribute %s", k))
		} else {
			delete(validAttributes, k)
		}
		t := v.Type()
		if t != validType {
			e := fmt.Errorf("invalid type for %s: wanted %s but found %s",
				k, typeRepr[validType], typeRepr[t])
			err = multierror.Append(err, e)
		}
	}
	if len(validAttributes) != 0 {
		for k, _ := range validAttributes {
			e := fmt.Errorf("required attribute %s is not defined", k)
			err = multierror.Append(err, e)
		}
	}
	return err
}

// validateImports ensures that imports are defined with string keys and string values. It also
// populates the compiler's `moduleImports` field once the validation is complete. Having the module
// imports populated makes it easy to validate that playbook tasks are using only imported modules
// later when validateBlocks runs.
func (c *Compiler) validateImports(imports *ValueExpr) error {
	var err error
	for k, v := range imports.Object {
		t := v.Type()
		if t != StringType {
			e := fmt.Errorf("invalid type for import %s: wanted String but found %s", k, typeRepr[t])
			err = multierror.Append(err, e)
		}
	}
	if err != nil {
		return err
	}
	// Populate the compiler's ModuleImports field. Type assertions can be used since the type
	// has been validated to have string keys and values.
	// The return type of imports.ToGoValue() is map[string]interface{}, but we know the underlying
	// value of the interface{} is a string.
	importMap := imports.ToGoValue().(map[string]interface{})
	for alias, path := range importMap {
		pathStr := path.(string)
		// module.NewModuleImport() validates the correct format of the path.
		moduleImport, e := module.NewModuleImport(alias, pathStr)
		if e != nil {
			err = multierror.Append(err, e)
		} else {
			c.moduleImports[alias] = moduleImport
		}
	}
	return err
}

// validateInputSchema ensures that the `input-schema` playbook attribute is a valid JSON schema.
func (c *Compiler) validateInputSchema(inputSchema *ValueExpr) error {
	t := inputSchema.Type()
	if t != ObjectType {
		return fmt.Errorf("invalid type for input schema: wanted Object but found %s", typeRepr[t])
	}
	schemaLoader := gojsonschema.NewSchemaLoader()
	schemaLoader.Validate = true
	_, err := schemaLoader.Compile(gojsonschema.NewGoLoader(inputSchema.Object.ToGoValue()))
	return err
}

// validateBlocks ensures that block tasks refer to modules that have been imported.
func (c *Compiler) validateBlocks(blocks []*BlockExpr) error {
	var err error
	for _, block := range blocks {
		for _, task := range block.Body {
			_, ok := c.moduleImports[task.ModuleName]
			if !ok {
				err = fmt.Errorf("unkown module %s", task.ModuleName)
			}
		}
		if block.Rescue != nil {
			for _, task := range block.Rescue {
				_, ok := c.moduleImports[task.ModuleName]
				if !ok {
					err = fmt.Errorf("unkown module %s", task.ModuleName)
				}
			}
		}
		if block.Always != nil {
			for _, task := range block.Always {
				_, ok := c.moduleImports[task.ModuleName]
				if !ok {
					err = fmt.Errorf("unkown module %s", task.ModuleName)
				}
			}
		}
	}
	return err
}

// Compile returns an `*ast.File` which can be formatted into Go using `go/format` or `go/printer`.
// The Load() method must be called before this.
func (c *Compiler) Compile() *ast.File {
	file := &ast.File{
		Name: &ast.Ident{
			Name: maine,
		},
		Decls: []ast.Decl{
			c.genDecl_import(),
			c.funcDecl_main(),
		},
		Package: 1,
	}
	return file
}

// genDecl_import creates an `*ast.GenDecl` for all of the playbook's imports.
func (c *Compiler) genDecl_import() *ast.GenDecl {
	specs := []ast.Spec{
		importSpec("github.com/cloudboss/unobin/pkg/functions"),
		importSpec("github.com/cloudboss/unobin/pkg/module"),
		importSpec("github.com/cloudboss/unobin/pkg/playbook"),
		importSpec("github.com/cloudboss/unobin/pkg/task"),
		importSpec("github.com/cloudboss/unobin/pkg/types"),
	}
	for _, value := range c.moduleImports {
		specs = append(specs, importSpec(value.GoImportPath))
	}
	return &ast.GenDecl{
		Tok:   token.IMPORT,
		Specs: specs,
	}
}

// funcDecl_main creates an `*ast.FuncDecl` for the playbook's `main` function.
func (c *Compiler) funcDecl_main() *ast.FuncDecl {
	return &ast.FuncDecl{
		Name: &ast.Ident{Name: maine},
		Type: &ast.FuncType{},
		Body: &ast.BlockStmt{
			List: []ast.Stmt{
				c.assignStmt_ctx(),
				c.assignStmt_pb(),
				&ast.ExprStmt{
					X: &ast.CallExpr{
						Fun: &ast.Ident{Name: startCLIMethod},
					},
				},
			},
		},
	}
}

// assignStmt_ctx creates an `*ast.AssignStmt` for the main function's `types.Context`.
func (c *Compiler) assignStmt_ctx() *ast.AssignStmt {
	return &ast.AssignStmt{
		Tok: token.DEFINE,
		Lhs: []ast.Expr{
			&ast.Ident{Name: ctxVar},
		},
		Rhs: []ast.Expr{
			&ast.UnaryExpr{
				Op: token.AND,
				X: &ast.CompositeLit{
					Type: &ast.Ident{Name: ctxQualifiedIdentifier},
					Elts: []ast.Expr{
						&ast.KeyValueExpr{
							Key: &ast.Ident{Name: varsField},
							Value: &ast.CompositeLit{
								Type: &ast.MapType{
									Key:   &ast.Ident{Name: stringType},
									Value: &ast.Ident{Name: interfaceType},
								},
							},
						},
						&ast.KeyValueExpr{
							Key: &ast.Ident{Name: stateField},
							Value: &ast.CompositeLit{
								Type: &ast.MapType{
									Key:   &ast.Ident{Name: stringType},
									Value: &ast.Ident{Name: interfaceType},
								},
							},
						},
					},
				},
			},
		},
	}
}

// assignStmt_pb creates an `*ast.AssignStmt` for the main function's `playbook.Playbook`.
func (c *Compiler) assignStmt_pb() *ast.AssignStmt {
	return &ast.AssignStmt{
		Tok: token.DEFINE,
		Lhs: []ast.Expr{
			&ast.Ident{Name: pbVar},
		},
		Rhs: []ast.Expr{
			&ast.CompositeLit{
				Type: &ast.Ident{Name: playbookQualifiedIdentifier},
				Elts: []ast.Expr{
					&ast.KeyValueExpr{
						Key:   &ast.Ident{Name: nameField},
						Value: c.grammar.uast.Attributes[nameAttr].ToGoAST(),
					},
					&ast.KeyValueExpr{
						Key:   &ast.Ident{Name: descriptionField},
						Value: c.grammar.uast.Attributes[descriptionAttr].ToGoAST(),
					},
					&ast.KeyValueExpr{
						Key:   &ast.Ident{Name: ctxField},
						Value: &ast.BasicLit{Kind: token.STRING, Value: ctxVar},
					},
					&ast.KeyValueExpr{
						Key:   &ast.Ident{Name: inputSchemaField},
						Value: c.grammar.uast.Attributes[inputSchemaAttr].ToGoAST(),
					},
					&ast.KeyValueExpr{
						Key:   &ast.Ident{Name: tasksField},
						Value: c.compositeLit_tasks(),
					},
				},
			},
		},
	}
}

// compositeLit_tasks creates an `*ast.CompositeLit` for the playbook's `*task.Task` array.
func (c *Compiler) compositeLit_tasks() *ast.CompositeLit {
	taskExprs := []ast.Expr{}
	for _, block := range c.grammar.uast.Blocks {
		for _, task := range block.Body {
			taskExpr := &ast.CompositeLit{Elts: []ast.Expr{}}
			taskExpr.Elts = append(taskExpr.Elts, &ast.KeyValueExpr{
				Key:   &ast.Ident{Name: nameField},
				Value: &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(task.Name)},
			})
			taskExpr.Elts = append(taskExpr.Elts, &ast.KeyValueExpr{
				Key:   &ast.Ident{Name: unwrapField},
				Value: c.funcLit_unwrap(task),
			})
			// TODO: move this up to the block level after refactoring
			// playbook structure to operate on an array of blocks containing
			// tasks instead of just an array of tasks.
			if when, ok := block.Attributes[whenKey]; ok {
				taskExpr.Elts = append(taskExpr.Elts, c.compileWhenExpr(when))
			}
			taskExprs = append(taskExprs, taskExpr)
		}
	}
	return &ast.CompositeLit{
		Type: &ast.ArrayType{Elt: &ast.StarExpr{X: &ast.Ident{Name: taskQualifiedIdentifier}}},
		Elts: taskExprs,
	}
}

// compileWhenExpr compiles the `*ast.KeyValueExpr` for the `When` field of a task.
func (c *Compiler) compileWhenExpr(when *ValueExpr) *ast.KeyValueExpr {
	return &ast.KeyValueExpr{
		Key: &ast.BasicLit{Kind: token.STRING, Value: whenField},
		Value: &ast.FuncLit{
			Type: &ast.FuncType{
				Results: &ast.FieldList{
					List: []*ast.Field{
						{Type: &ast.Ident{Name: boolType}},
						{Type: &ast.Ident{Name: errorType}},
					},
				},
			},
			Body: &ast.BlockStmt{
				List: []ast.Stmt{
					&ast.AssignStmt{
						Tok: token.DEFINE,
						Lhs: []ast.Expr{
							&ast.Ident{Name: whenKey},
						},
						Rhs: []ast.Expr{
							c.compileModuleExpr(when),
						},
					},
					&ast.ReturnStmt{
						Results: []ast.Expr{
							&ast.BasicLit{
								Kind:  token.STRING,
								Value: fmt.Sprintf("%s.Value", whenKey),
							},
							&ast.BasicLit{
								Kind:  token.STRING,
								Value: fmt.Sprintf("%s.Error", whenKey),
							},
						},
					},
				},
			},
		},
	}
}

// funcLit_unwrap creates an `*ast.FuncLit` for a playbook task's `Unwrap` field.
func (c *Compiler) funcLit_unwrap(task *TaskExpr) *ast.FuncLit {
	funcLit := ast.FuncLit{
		Type: &ast.FuncType{
			Results: &ast.FieldList{
				List: []*ast.Field{
					{Type: &ast.Ident{Name: moduleQualifiedIdentifier}},
					{Type: &ast.Ident{Name: errorType}},
				},
			},
		},
		Body: &ast.BlockStmt{},
	}
	moduleImport := c.moduleImports[task.ModuleName]
	funcLit.Body.List = []ast.Stmt{
		&ast.AssignStmt{
			Tok: token.DEFINE,
			Lhs: []ast.Expr{
				&ast.Ident{Name: modVar},
			},
			Rhs: []ast.Expr{
				&ast.UnaryExpr{
					Op: token.AND,
					X: &ast.CompositeLit{
						Type: &ast.Ident{
							Name: moduleImport.QualifiedIdentifier,
						},
					},
				},
			},
		},
	}
	for k, v := range task.ModuleParameters {
		stmts := c.moduleParamStmts(k, v)
		funcLit.Body.List = append(funcLit.Body.List, stmts...)
	}
	funcLit.Body.List = append(funcLit.Body.List, &ast.ReturnStmt{
		Results: []ast.Expr{
			&ast.Ident{Name: modVar},
			&ast.Ident{Name: nilValue},
		},
	})
	return &funcLit
}

// compileModuleExpr is given a `*ValueExpr` parsed from the playbook and produces an `*ast.CallExpr`
// from it. If it encounters another function call as an argument, it recursively calls itself to
// compile it, otherwise it returns a CompositeLit for a literal value from the `functions` package.
func (c *Compiler) compileModuleExpr(value *ValueExpr) ast.Expr {
	switch value.Type() {
	case FunctionType:
		return value.ToGoAST()
	default:
		qualifiedIdentifier := fmt.Sprintf(functionsPackageTemplate, typeRepr[value.Type()])
		return &ast.CompositeLit{
			Type: &ast.BasicLit{Kind: token.STRING, Value: qualifiedIdentifier},
			Elts: []ast.Expr{
				value.ToGoAST(),
				&ast.BasicLit{Kind: token.STRING, Value: nilValue},
			},
		}
	}
}

// moduleParamStmts creates an `[]ast.Stmt` for a task's module parameters.
func (c *Compiler) moduleParamStmts(ident string, value *ValueExpr) []ast.Stmt {
	stmts := []ast.Stmt{}
	variable := util.KebabToCamel(ident)
	t := value.Type()
	if t == FunctionType {
		assignStmt := &ast.AssignStmt{
			Tok: token.DEFINE,
			Lhs: []ast.Expr{
				&ast.Ident{Name: variable},
			},
			Rhs: []ast.Expr{c.compileModuleExpr(value)},
		}
		stmts = append(stmts, assignStmt)
		errField := fmt.Sprintf("%s.Error", variable)
		ifErrStmt := &ast.IfStmt{
			Cond: &ast.BinaryExpr{
				Op: token.NEQ,
				X:  &ast.Ident{Name: errField},
				Y:  &ast.Ident{Name: nilValue},
			},
			Body: &ast.BlockStmt{
				List: []ast.Stmt{
					&ast.ReturnStmt{
						Results: []ast.Expr{
							&ast.Ident{Name: modVar},
							&ast.Ident{Name: errField},
						},
					},
				},
			},
		}
		stmts = append(stmts, ifErrStmt)
		valueField := fmt.Sprintf("%s.Value", variable)
		assignFieldStmt := &ast.AssignStmt{
			Tok: token.ASSIGN,
			Lhs: []ast.Expr{
				&ast.Ident{Name: fmt.Sprintf("mod.%s", util.KebabToPascal(ident))},
			},
			Rhs: []ast.Expr{
				&ast.Ident{Name: valueField},
			},
		}
		stmts = append(stmts, assignFieldStmt)
	} else {
		assignFieldStmt := &ast.AssignStmt{
			Tok: token.ASSIGN,
			Lhs: []ast.Expr{
				&ast.Ident{Name: fmt.Sprintf("mod.%s", util.KebabToPascal(ident))},
			},
			Rhs: []ast.Expr{
				value.ToGoAST(),
			},
		}
		stmts = append(stmts, assignFieldStmt)
	}
	return stmts
}

// importSpec creates an `*ast.ImportSpec` for a single import.
func importSpec(path string) *ast.ImportSpec {
	return &ast.ImportSpec{
		Path: &ast.BasicLit{Value: strconv.Quote(path)},
	}
}
