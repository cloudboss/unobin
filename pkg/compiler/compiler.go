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
	"go/token"
	"io/ioutil"
	"strconv"

	"github.com/cloudboss/unobin/pkg/module"
	"github.com/cloudboss/unobin/pkg/util"
	"github.com/dave/dst"
	"github.com/go-bindata/go-bindata"
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

// Compile returns a `*dst.File` which can be formatted into Go using `go/format` or `go/printer`.
// The Load() method must be called before this.
func (c *Compiler) Compile() *dst.File {
	file := &dst.File{
		Name: &dst.Ident{
			Name: maine,
		},
		Decls: []dst.Decl{
			c.genDecl_import(),
			// c.genDecl_var(),
			c.funcDecl_main(),
		},
	}
	return file
}

// genDecl_import creates a `*dst.GenDecl` for all of the playbook's imports.
func (c *Compiler) genDecl_import() *dst.GenDecl {
	specs := []dst.Spec{
		importSpec("fmt"),
		importSpec("os"),
		importSpec("github.com/cloudboss/unobin/pkg/functions"),
		importSpec("github.com/cloudboss/unobin/pkg/module"),
		importSpec("github.com/cloudboss/unobin/pkg/playbook"),
		importSpec("github.com/cloudboss/unobin/pkg/task"),
		importSpec("github.com/cloudboss/unobin/pkg/types"),
	}
	for _, value := range c.moduleImports {
		specs = append(specs, importSpec(value.GoImportPath))
	}
	return &dst.GenDecl{
		Tok:   token.IMPORT,
		Specs: specs,
	}
}

// genDecl_var creates a `*dst.GenDecl` for a pkger.Include() call assigned to a var `_`.
// This allows the playbook to bundle files in the playbook's resources directory.
// See https://godoc.org/github.com/markbates/pkger#Include.
func (c *Compiler) genDecl_var() *dst.GenDecl {
	return &dst.GenDecl{
		Tok: token.VAR,
		Specs: []dst.Spec{
			&dst.ValueSpec{
				Names: []*dst.Ident{
					&dst.Ident{Name: underscoreVar},
				},
				Values: []dst.Expr{
					&dst.CallExpr{
						Fun: &dst.BasicLit{
							Kind:  token.STRING,
							Value: includeQualifiedIdentifier,
						},
						Args: []dst.Expr{
							&dst.BasicLit{
								Kind:  token.STRING,
								Value: strconv.Quote(resources),
							},
						},
					},
				},
			},
		},
	}
}

// funcDecl_main creates a `*dst.FuncDecl` for the playbook's `main` function.
func (c *Compiler) funcDecl_main() *dst.FuncDecl {
	return &dst.FuncDecl{
		Name: &dst.Ident{Name: maine},
		Type: &dst.FuncType{},
		Body: &dst.BlockStmt{
			List: []dst.Stmt{
				c.assignStmt_ctx(),
				c.assignStmt_resourceNames(),
				c.assignStmt_resourcesLen(),
				c.assignStmt_resources(),
				c.rangeStmt_resources(),
				c.assignStmt_pb(),
				&dst.ExprStmt{
					X: &dst.CallExpr{
						Fun: &dst.Ident{Name: startCLIMethod},
					},
				},
			},
		},
	}
}

// assignStmt_resourceNames creates a `*dst.AssignStmt` for the main function's `resourceNames` variable.
func (c *Compiler) assignStmt_resourceNames() *dst.AssignStmt {
	return &dst.AssignStmt{
		Tok: token.DEFINE,
		Lhs: []dst.Expr{
			&dst.Ident{Name: resourceNamesVar},
		},
		Rhs: []dst.Expr{
			&dst.CallExpr{Fun: &dst.Ident{Name: assetNamesFunc}},
		},
	}
}

// assignStmt_resourcesLen creates a `*dst.AssignStmt` for the main function's `resourcesLen` variable.
func (c *Compiler) assignStmt_resourcesLen() *dst.AssignStmt {
	return &dst.AssignStmt{
		Tok: token.DEFINE,
		Lhs: []dst.Expr{&dst.Ident{Name: resourcesLenVar}},
		Rhs: []dst.Expr{
			&dst.CallExpr{
				Fun: &dst.Ident{Name: "len"},
				Args: []dst.Expr{
					&dst.Ident{Name: resourceNamesVar},
				},
			},
		},
	}
}

// assignStmt_resources creates a `*dst.AssignStmt` for the main function's `resources` variable.
func (c *Compiler) assignStmt_resources() *dst.AssignStmt {
	return &dst.AssignStmt{
		Tok: token.DEFINE,
		Lhs: []dst.Expr{&dst.Ident{Name: resources}},
		Rhs: []dst.Expr{
			&dst.CallExpr{
				Fun: &dst.Ident{Name: makeFunc},
				Args: []dst.Expr{
					&dst.Ident{
						Name: fmt.Sprintf("[]%s", resourceQualifiedIdentifier),
					},
					&dst.Ident{Name: resourcesLenVar},
				},
			},
		},
	}
}

// rangeStmt_resources creates a `*dst.RangeStmt` to fill the main function's `resources` variable.
func (c *Compiler) rangeStmt_resources() *dst.RangeStmt {
	return &dst.RangeStmt{
		Key:   &dst.Ident{Name: iVar},
		Value: &dst.Ident{Name: resourceVar},
		X:     &dst.Ident{Name: resourceNamesVar},
		Tok:   token.DEFINE,
		Body: &dst.BlockStmt{
			List: []dst.Stmt{
				&dst.AssignStmt{
					Tok: token.DEFINE,
					Lhs: []dst.Expr{
						&dst.Ident{Name: infoVar},
						&dst.Ident{Name: errVar},
					},
					Rhs: []dst.Expr{
						&dst.CallExpr{
							Fun: &dst.Ident{Name: assetInfoFunc},
							Args: []dst.Expr{
								&dst.Ident{Name: resourceVar},
							},
						},
					},
				},
				&dst.IfStmt{
					Cond: &dst.BinaryExpr{
						Op: token.NEQ,
						X:  &dst.Ident{Name: errVar},
						Y:  &dst.Ident{Name: nilValue},
					},
					Body: &dst.BlockStmt{
						List: []dst.Stmt{
							&dst.ExprStmt{
								X: &dst.CallExpr{
									Fun: &dst.Ident{Name: printfQualifiedIdentifier},
									Args: []dst.Expr{
										&dst.BasicLit{
											Kind:  token.STRING,
											Value: strconv.Quote("failed to get info for resource %s: %s"),
										},
										&dst.Ident{Name: resourceVar},
										&dst.Ident{Name: errVar},
									},
								},
							},
							&dst.ExprStmt{
								X: &dst.CallExpr{
									Fun: &dst.Ident{Name: exitQualifiedIdentifier},
									Args: []dst.Expr{
										&dst.BasicLit{
											Kind:  token.INT,
											Value: "1",
										},
									},
								},
							},
						},
					},
				},
				&dst.AssignStmt{
					Tok: token.DEFINE,
					Lhs: []dst.Expr{
						&dst.Ident{Name: contentsVar},
						&dst.Ident{Name: errVar},
					},
					Rhs: []dst.Expr{
						&dst.CallExpr{
							Fun: &dst.Ident{Name: assetFunc},
							Args: []dst.Expr{
								&dst.Ident{Name: resourceVar},
							},
						},
					},
				},
				&dst.IfStmt{
					Cond: &dst.BinaryExpr{
						Op: token.NEQ,
						X:  &dst.Ident{Name: errVar},
						Y:  &dst.Ident{Name: nilValue},
					},
					Body: &dst.BlockStmt{
						List: []dst.Stmt{
							&dst.ExprStmt{
								X: &dst.CallExpr{
									Fun: &dst.Ident{Name: printfQualifiedIdentifier},
									Args: []dst.Expr{
										&dst.BasicLit{
											Kind:  token.STRING,
											Value: strconv.Quote("failed to get contents of resource %s: %s"),
										},
										&dst.Ident{Name: resourceVar},
										&dst.Ident{Name: errVar},
									},
								},
							},
							&dst.ExprStmt{
								X: &dst.CallExpr{
									Fun: &dst.Ident{Name: exitQualifiedIdentifier},
									Args: []dst.Expr{
										&dst.BasicLit{
											Kind:  token.INT,
											Value: "1",
										},
									},
								},
							},
						},
					},
				},
				&dst.AssignStmt{
					Tok: token.ASSIGN,
					Lhs: []dst.Expr{
						&dst.IndexExpr{
							X:     &dst.Ident{Name: resources},
							Index: &dst.Ident{Name: iVar},
						},
					},
					Rhs: []dst.Expr{
						&dst.CompositeLit{
							Type: &dst.Ident{Name: resourceQualifiedIdentifier},
							Elts: []dst.Expr{
								&dst.KeyValueExpr{
									Key:   &dst.Ident{Name: pathField},
									Value: &dst.Ident{Name: resourceVar},
								},
								&dst.KeyValueExpr{
									Key:   &dst.Ident{Name: infoField},
									Value: &dst.Ident{Name: infoVar},
								},
								&dst.KeyValueExpr{
									Key:   &dst.Ident{Name: contentsField},
									Value: &dst.Ident{Name: contentsVar},
								},
							},
						},
					},
				},
			},
		},
	}
}

// assignStmt_ctx creates a `*dst.AssignStmt` for the main function's `types.Context`.
func (c *Compiler) assignStmt_ctx() *dst.AssignStmt {
	return &dst.AssignStmt{
		Tok: token.DEFINE,
		Lhs: []dst.Expr{
			&dst.Ident{Name: ctxVar},
		},
		Rhs: []dst.Expr{
			&dst.UnaryExpr{
				Op: token.AND,
				X: &dst.CompositeLit{
					Type: &dst.Ident{Name: ctxQualifiedIdentifier},
					Elts: []dst.Expr{
						&dst.KeyValueExpr{
							Decs: dst.KeyValueExprDecorations{
								NodeDecs: dst.NodeDecs{
									Before: dst.NewLine,
									After:  dst.NewLine,
								},
							},
							Key: &dst.Ident{Name: varsField},
							Value: &dst.CompositeLit{
								Type: &dst.MapType{
									Key:   &dst.Ident{Name: stringType},
									Value: &dst.Ident{Name: interfaceType},
								},
							},
						},
						&dst.KeyValueExpr{
							Decs: dst.KeyValueExprDecorations{NodeDecs: dst.NodeDecs{After: dst.NewLine}},
							Key:  &dst.Ident{Name: stateField},
							Value: &dst.CompositeLit{
								Type: &dst.MapType{
									Key:   &dst.Ident{Name: stringType},
									Value: &dst.Ident{Name: interfaceType},
								},
							},
						},
					},
				},
			},
		},
	}
}

// assignStmt_pb creates a `*dst.AssignStmt` for the main function's `playbook.Playbook`.
func (c *Compiler) assignStmt_pb() *dst.AssignStmt {
	return &dst.AssignStmt{
		Tok: token.DEFINE,
		Lhs: []dst.Expr{
			&dst.Ident{Name: pbVar},
		},
		Rhs: []dst.Expr{
			&dst.CompositeLit{
				Type: &dst.Ident{Name: playbookQualifiedIdentifier},
				Elts: []dst.Expr{
					&dst.KeyValueExpr{
						Decs: dst.KeyValueExprDecorations{
							NodeDecs: dst.NodeDecs{
								Before: dst.NewLine,
								After:  dst.NewLine,
							},
						},
						Key:   &dst.Ident{Name: nameField},
						Value: c.grammar.uast.Attributes[nameAttr].ToGoAST(),
					},
					&dst.KeyValueExpr{
						Decs:  dst.KeyValueExprDecorations{NodeDecs: dst.NodeDecs{After: dst.NewLine}},
						Key:   &dst.Ident{Name: descriptionField},
						Value: c.grammar.uast.Attributes[descriptionAttr].ToGoAST(),
					},
					&dst.KeyValueExpr{
						Decs:  dst.KeyValueExprDecorations{NodeDecs: dst.NodeDecs{After: dst.NewLine}},
						Key:   &dst.Ident{Name: ctxField},
						Value: &dst.BasicLit{Kind: token.STRING, Value: ctxVar},
					},
					&dst.KeyValueExpr{
						Decs:  dst.KeyValueExprDecorations{NodeDecs: dst.NodeDecs{After: dst.NewLine}},
						Key:   &dst.Ident{Name: inputSchemaField},
						Value: c.grammar.uast.Attributes[inputSchemaAttr].ToGoAST(),
					},
					&dst.KeyValueExpr{
						Decs:  dst.KeyValueExprDecorations{NodeDecs: dst.NodeDecs{After: dst.NewLine}},
						Key:   &dst.Ident{Name: resourcesField},
						Value: &dst.Ident{Name: resources},
					},
					&dst.KeyValueExpr{
						Decs:  dst.KeyValueExprDecorations{NodeDecs: dst.NodeDecs{After: dst.NewLine}},
						Key:   &dst.Ident{Name: tasksField},
						Value: c.compositeLit_tasks(),
					},
				},
			},
		},
	}
}

// compositeLit_tasks creates a `*dst.CompositeLit` for the playbook's `*task.Task` array.
func (c *Compiler) compositeLit_tasks() *dst.CompositeLit {
	taskExprs := []dst.Expr{}
	for _, block := range c.grammar.uast.Blocks {
		for _, task := range block.Body {
			taskExpr := &dst.CompositeLit{
				Decs: dst.CompositeLitDecorations{
					NodeDecs: dst.NodeDecs{
						Before: dst.NewLine,
						After:  dst.NewLine,
					},
				},
				Elts: []dst.Expr{},
			}
			taskExpr.Elts = append(taskExpr.Elts, &dst.KeyValueExpr{
				Decs: dst.KeyValueExprDecorations{
					NodeDecs: dst.NodeDecs{
						Before: dst.NewLine,
						After:  dst.NewLine,
					},
				},
				Key:   &dst.Ident{Name: nameField},
				Value: &dst.BasicLit{Kind: token.STRING, Value: strconv.Quote(task.Name)},
			})
			taskExpr.Elts = append(taskExpr.Elts, &dst.KeyValueExpr{
				Decs:  dst.KeyValueExprDecorations{NodeDecs: dst.NodeDecs{After: dst.NewLine}},
				Key:   &dst.Ident{Name: unwrapField},
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
	return &dst.CompositeLit{
		Type: &dst.ArrayType{Elt: &dst.StarExpr{X: &dst.Ident{Name: taskQualifiedIdentifier}}},
		Elts: taskExprs,
	}
}

// compileWhenExpr compiles the `*dst.KeyValueExpr` for the `When` field of a task.
func (c *Compiler) compileWhenExpr(when *ValueExpr) *dst.KeyValueExpr {
	return &dst.KeyValueExpr{
		Key: &dst.BasicLit{Kind: token.STRING, Value: whenField},
		Value: &dst.FuncLit{
			Type: &dst.FuncType{
				Results: &dst.FieldList{
					List: []*dst.Field{
						{Type: &dst.Ident{Name: boolType}},
						{Type: &dst.Ident{Name: errorType}},
					},
				},
			},
			Body: &dst.BlockStmt{
				List: []dst.Stmt{
					&dst.AssignStmt{
						Tok: token.DEFINE,
						Lhs: []dst.Expr{
							&dst.Ident{Name: whenKey},
						},
						Rhs: []dst.Expr{
							c.compileModuleExpr(when),
						},
					},
					&dst.ReturnStmt{
						Results: []dst.Expr{
							&dst.BasicLit{
								Kind:  token.STRING,
								Value: fmt.Sprintf("%s.Value", whenKey),
							},
							&dst.BasicLit{
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

// funcLit_unwrap creates a `*dst.FuncLit` for a playbook task's `Unwrap` field.
func (c *Compiler) funcLit_unwrap(task *TaskExpr) *dst.FuncLit {
	funcLit := dst.FuncLit{
		Type: &dst.FuncType{
			Results: &dst.FieldList{
				List: []*dst.Field{
					{Type: &dst.Ident{Name: moduleQualifiedIdentifier}},
					{Type: &dst.Ident{Name: errorType}},
				},
			},
		},
		Body: &dst.BlockStmt{},
	}
	moduleImport := c.moduleImports[task.ModuleName]
	funcLit.Body.List = []dst.Stmt{
		&dst.AssignStmt{
			Tok: token.DEFINE,
			Lhs: []dst.Expr{
				&dst.Ident{Name: modVar},
			},
			Rhs: []dst.Expr{
				&dst.UnaryExpr{
					Op: token.AND,
					X: &dst.CompositeLit{
						Type: &dst.Ident{
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
	funcLit.Body.List = append(funcLit.Body.List, &dst.ReturnStmt{
		Results: []dst.Expr{
			&dst.Ident{Name: modVar},
			&dst.Ident{Name: nilValue},
		},
	})
	return &funcLit
}

// compileModuleExpr is given a `*ValueExpr` parsed from the playbook and produces a `*dst.CallExpr`
// from it. If it encounters another function call as an argument, it recursively calls itself to
// compile it, otherwise it returns a CompositeLit for a literal value from the `functions` package.
func (c *Compiler) compileModuleExpr(value *ValueExpr) dst.Expr {
	switch value.Type() {
	case FunctionType:
		return value.ToGoAST()
	default:
		qualifiedIdentifier := fmt.Sprintf(functionsPackageTemplate, typeRepr[value.Type()])
		return &dst.CompositeLit{
			Type: &dst.BasicLit{Kind: token.STRING, Value: qualifiedIdentifier},
			Elts: []dst.Expr{
				value.ToGoAST(),
				&dst.BasicLit{Kind: token.STRING, Value: nilValue},
			},
		}
	}
}

// moduleParamStmts creates a `[]dst.Stmt` for a task's module parameters.
func (c *Compiler) moduleParamStmts(ident string, value *ValueExpr) []dst.Stmt {
	stmts := []dst.Stmt{}
	variable := util.KebabToCamel(ident)
	t := value.Type()
	if t == FunctionType {
		assignStmt := &dst.AssignStmt{
			Tok: token.DEFINE,
			Lhs: []dst.Expr{
				&dst.Ident{Name: variable},
			},
			Rhs: []dst.Expr{c.compileModuleExpr(value)},
		}
		stmts = append(stmts, assignStmt)
		errField := fmt.Sprintf("%s.Error", variable)
		ifErrStmt := &dst.IfStmt{
			Cond: &dst.BinaryExpr{
				Op: token.NEQ,
				X:  &dst.Ident{Name: errField},
				Y:  &dst.Ident{Name: nilValue},
			},
			Body: &dst.BlockStmt{
				List: []dst.Stmt{
					&dst.ReturnStmt{
						Results: []dst.Expr{
							&dst.Ident{Name: modVar},
							&dst.Ident{Name: errField},
						},
					},
				},
			},
		}
		stmts = append(stmts, ifErrStmt)
		valueField := fmt.Sprintf("%s.Value", variable)
		assignFieldStmt := &dst.AssignStmt{
			Tok: token.ASSIGN,
			Lhs: []dst.Expr{
				&dst.Ident{Name: fmt.Sprintf("mod.%s", util.KebabToPascal(ident))},
			},
			Rhs: []dst.Expr{
				&dst.Ident{Name: valueField},
			},
		}
		stmts = append(stmts, assignFieldStmt)
	} else {
		assignFieldStmt := &dst.AssignStmt{
			Tok: token.ASSIGN,
			Lhs: []dst.Expr{
				&dst.Ident{Name: fmt.Sprintf("mod.%s", util.KebabToPascal(ident))},
			},
			Rhs: []dst.Expr{
				value.ToGoAST(),
			},
		}
		stmts = append(stmts, assignFieldStmt)
	}
	return stmts
}

func (c *Compiler) PackageResources() error {
	config := &bindata.Config{
		Prefix:  resources,
		Package: maine,
		Input: []bindata.InputConfig{
			{
				Path:      resources,
				Recursive: true,
			},
		},
		Output: "resources.go",
	}
	return bindata.Translate(config)
}

// importSpec creates a `*dst.ImportSpec` for a single import.
func importSpec(path string) *dst.ImportSpec {
	return &dst.ImportSpec{
		Path: &dst.BasicLit{Value: strconv.Quote(path)},
	}
}
