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

package compiler

const (
	alwaysField                 = "Always"
	arrayType                   = "Array"
	assetFunc                   = "Asset"
	assetInfoFunc               = "AssetInfo"
	assetNamesFunc              = "AssetNames"
	bodyField                   = "Body"
	boolType                    = "bool"
	contentsField               = "Contents"
	contentsVar                 = "contents"
	ctxField                    = "Context"
	ctxVar                      = "ctx"
	ctxQualifiedIdentifier      = "types.Context"
	descriptionAttr             = "description"
	descriptionField            = "Description"
	errorType                   = "error"
	errVar                      = "err"
	exitQualifiedIdentifier     = "os.Exit"
	expandArrayFunc             = "ExpandArray"
	expandObjectFunc            = "ExpandObject"
	functionsPackageTemplate    = "functions.%s"
	importsAttr                 = "imports"
	includeQualifiedIdentifier  = "pkger.Include"
	infoField                   = "Info"
	infoVar                     = "info"
	inputSchemaAttr             = "input-schema"
	inputSchemaField            = "InputSchema"
	interfaceType               = "interface{}"
	invalidIdentifier           = "InvalidIdentifier"
	iVar                        = "i"
	kVar                        = "k"
	maine                       = "main"
	makeFunc                    = "make"
	moduleQualifiedIdentifier   = "module.Module"
	modVar                      = "mod"
	nameField                   = "Name"
	nameAttr                    = "name"
	nilValue                    = "nil"
	pathField                   = "Path"
	pbVar                       = "pb"
	playbookQualifiedIdentifier = "playbook.Playbook"
	printfQualifiedIdentifier   = "fmt.Printf"
	rescueField                 = "Rescue"
	resourceQualifiedIdentifier = "playbook.Resource"
	resourceNamesVar            = "resourceNames"
	resourceVar                 = "resource"
	resources                   = "resources"
	resourcesField              = "Resources"
	resourcesLenVar             = "resourcesLen"
	startCLIMethod              = "pb.StartCLI"
	stateField                  = "State"
	stringType                  = "string"
	taskQualifiedIdentifier     = "task.Task"
	tasksField                  = "Tasks"
	underscoreVar               = "_"
	unwrapModuleField           = "UnwrapModule"
	varsField                   = "Vars"
	vVar                        = "v"
	whenField                   = "When"
	whenKey                     = "when"
)
