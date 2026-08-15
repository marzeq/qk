package loader

import (
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/ir"
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/sema"
	"github.com/marzeq/qk/shared"
	qktarget "github.com/marzeq/qk/target"
	"github.com/marzeq/qk/tokeniser"
	"github.com/marzeq/qk/types"
)

type StageReleaseMode uint8

const (
	StageDebug StageReleaseMode = iota
	StageRelease
)

type StageConfig struct {
	TargetTriple string
	Sysroot      string
	ReleaseMode  StageReleaseMode
	NoLibc       bool
	NoStdlib     bool
}

// StageEnvironment supplies already selected dependency modules to compile-time
// evaluation. Staging clones these modules before running the ordinary semantic
// and IR pipelines, so it gets normal import resolution without mutating the
// frontend's real semantic state.
type StageEnvironment struct {
	Modules                map[string]*ModuleInfo
	Order                  []string
	TrustedStandardLibrary bool
}

type stageKind uint8

const (
	stageBool stageKind = iota
	stageInteger
	stageFloat
	stageEnum
	stageEnumLiteral
)

type stageValue struct {
	kind     stageKind
	boolean  bool
	integer  *big.Int
	floating string
	typ      types.Type
	domain   string
	name     string
}

type stageSelector struct {
	config      StageConfig
	module      string
	bindings    map[string]stageValue
	evaluated   map[parser.ExpressionNode]stageValue
	unresolved  map[parser.ExpressionNode]struct{}
	needsIR     bool
	syntheticID int
	environment *StageEnvironment
}

// SelectCompileTime lowers unresolved staged expressions through the ordinary
// semantic and IR pipelines, evaluates them, and removes parsed when nodes.
func SelectCompileTime(root *parser.RootNode, module string, config StageConfig) error {
	return selectCompileTime(root, module, config, nil)
}

func selectCompileTime(root *parser.RootNode, module string, config StageConfig, environment *StageEnvironment) error {
	selector := &stageSelector{
		config: config, module: module,
		bindings: make(map[string]stageValue), evaluated: make(map[parser.ExpressionNode]stageValue),
		unresolved: make(map[parser.ExpressionNode]struct{}), environment: environment,
	}
	selector.primeBindings(root.Body)
	body, err := selector.selectBody(root.Body)
	if err != nil {
		return err
	}
	root.Body = body
	if selector.needsIR {
		if err := selector.evaluateUnresolved(root); err != nil {
			return err
		}
		selector.needsIR = false
		selector.unresolved = make(map[parser.ExpressionNode]struct{})
		body, err = selector.selectBody(root.Body)
		if err != nil {
			return err
		}
		root.Body = body
	}
	if selector.needsIR {
		return fmt.Errorf("compile-time selection left an unresolved when condition")
	}
	return nil
}

// SelectCompileTimeRoots selects a module's parsed files together, preserving
// same-module compile-time visibility independently of file discovery order.
func SelectCompileTimeRoots(roots []*parser.RootNode, module string, config StageConfig) error {
	return SelectCompileTimeRootsWithEnvironment(roots, module, config, nil)
}

// SelectCompileTimeRootsWithEnvironment selects a module while making its
// already selected dependencies available through ordinary imports.
func SelectCompileTimeRootsWithEnvironment(roots []*parser.RootNode, module string, config StageConfig, environment *StageEnvironment) error {
	if len(roots) == 0 {
		return nil
	}
	combined := &parser.RootNode{Loc: roots[0].Loc}
	owners := make(map[string]*parser.RootNode, len(roots))
	for _, root := range roots {
		combined.Body = append(combined.Body, root.Body...)
		owners[root.Loc.FilePath] = root
		root.Body = nil
	}
	if err := selectCompileTime(combined, module, config, environment); err != nil {
		return err
	}
	for _, node := range combined.Body {
		owner := owners[node.GetLoc().FilePath]
		if owner == nil {
			owner = roots[0]
		}
		owner.Body = append(owner.Body, node)
	}
	return nil
}

func (s *stageSelector) primeBindings(body []parser.Node) {
	remaining := make(map[*parser.DeclarationNode]bool)
	for _, node := range body {
		if declaration, ok := node.(*parser.DeclarationNode); ok && declaration.Comptime {
			remaining[declaration] = true
		}
	}
	for progress := true; progress; {
		progress = false
		for declaration := range remaining {
			value, resolved, err := s.evaluate(declaration.Value)
			if err != nil || !resolved {
				continue
			}
			s.bindings[declaration.Name] = value
			delete(remaining, declaration)
			progress = true
		}
	}
}

func (s *stageSelector) selectBody(body []parser.Node) ([]parser.Node, error) {
	var result []parser.Node
	for _, node := range body {
		selected, err := s.selectNode(node)
		if err != nil {
			return nil, err
		}
		result = append(result, selected...)
	}
	return result, nil
}

func (s *stageSelector) selectNode(node parser.Node) ([]parser.Node, error) {
	switch node := node.(type) {
	case *parser.WhenNode:
		selected, resolved, err := s.selectWhen(node)
		if err != nil {
			return nil, err
		}
		if !resolved {
			return []parser.Node{node}, nil
		}
		return selected, nil
	case *parser.CompilerDirectiveNode:
		if err := s.evaluateDirective(node); err != nil {
			return nil, err
		}
		if _, unresolved := s.unresolved[node.Condition]; unresolved {
			return []parser.Node{node}, nil
		}
		return nil, nil
	case *parser.ModuleNode:
		for index, attribute := range node.Attributes {
			parsed, ok := attribute.(parser.ParsedLinkAttribute)
			if !ok {
				continue
			}
			links, resolved, err := s.selectLinkItems(parsed.Items)
			if err != nil {
				return nil, err
			}
			if resolved {
				node.Attributes[index] = attributes.ModuleAttributeLink{Links: links}
			}
		}
	case *parser.DeclarationNode:
		if node.Comptime {
			if value, resolved, err := s.evaluate(node.Value); err != nil {
				return nil, err
			} else if resolved {
				s.bindings[node.Name] = value
				node.Value = stageLiteral(value, node.Value.GetLoc())
				if node.TypeNode == nil {
					node.TypeNode = stageTypeNode(value, node.NameLoc)
				}
			} else {
				s.needsIR = true
				s.unresolved[node.Value] = struct{}{}
			}
		}
		var err error
		node.Value, err = s.selectExpression(node.Value)
		if err != nil {
			return nil, err
		}
		node.TypeNode, err = s.selectType(node.TypeNode)
		if err != nil {
			return nil, err
		}
	case *parser.TypeAliasNode:
		var err error
		node.Type, err = s.selectType(node.Type)
		if err != nil {
			return nil, err
		}
	case *parser.FunctionDefNode:
		var err error
		for _, arg := range node.Args {
			arg.Type, err = s.selectType(arg.Type)
			if err != nil {
				return nil, err
			}
			arg.Default, err = s.selectExpression(arg.Default)
			if err != nil {
				return nil, err
			}
		}
		node.RetTypeNode, err = s.selectType(node.RetTypeNode)
		if err != nil {
			return nil, err
		}
		if block, ok := node.Body.(*parser.BlockNode); ok {
			selected, err := s.selectBody(block.Body)
			if err != nil {
				return nil, err
			}
			block.Body = selected
		} else if expression, ok := node.Body.(parser.ExpressionNode); ok {
			node.Body, err = s.selectExpression(expression)
			if err != nil {
				return nil, err
			}
		}
	case *parser.BlockNode:
		selected, err := s.selectBody(node.Body)
		if err != nil {
			return nil, err
		}
		node.Body = selected
	case *parser.IfNode:
		var err error
		node.IfBranch.Condition, err = s.selectExpression(node.IfBranch.Condition)
		if err != nil {
			return nil, err
		}
		if _, err = s.selectNode(node.IfBranch.Node); err != nil {
			return nil, err
		}
		for index := range node.ElseIfBranches {
			branch := &node.ElseIfBranches[index]
			branch.Condition, err = s.selectExpression(branch.Condition)
			if err != nil {
				return nil, err
			}
			if _, err = s.selectNode(branch.Node); err != nil {
				return nil, err
			}
		}
		if node.ElseBranch != nil {
			if _, err = s.selectNode(node.ElseBranch); err != nil {
				return nil, err
			}
		}
	case *parser.ForNode:
		selected, err := s.selectBody(node.ExprsOrStmts)
		if err != nil {
			return nil, err
		}
		node.ExprsOrStmts = selected
		if _, err = s.selectNode(node.Body); err != nil {
			return nil, err
		}
	case *parser.RangeForNode:
		var err error
		node.Start, err = s.selectExpression(node.Start)
		if err != nil {
			return nil, err
		}
		node.End, err = s.selectExpression(node.End)
		if err != nil {
			return nil, err
		}
		if _, err = s.selectNode(node.Body); err != nil {
			return nil, err
		}
	case *parser.ForEachNode:
		var err error
		node.Iterable, err = s.selectExpression(node.Iterable)
		if err != nil {
			return nil, err
		}
		if _, err = s.selectNode(node.Body); err != nil {
			return nil, err
		}
	case *parser.MatchNode:
		var err error
		for index := range node.Subjects {
			node.Subjects[index], err = s.selectExpression(node.Subjects[index])
			if err != nil {
				return nil, err
			}
		}
		for index := range node.Arms {
			arm := &node.Arms[index]
			arm.Guard, err = s.selectExpression(arm.Guard)
			if err != nil {
				return nil, err
			}
			arm.Body, err = s.selectExpression(arm.Body)
			if err != nil {
				return nil, err
			}
		}
	case *parser.ControlKeywordNode:
		var err error
		node.ReturnValue, err = s.selectExpression(node.ReturnValue)
		if err != nil {
			return nil, err
		}
		for index := range node.ReturnValues {
			node.ReturnValues[index], err = s.selectExpression(node.ReturnValues[index])
			if err != nil {
				return nil, err
			}
		}
	case *parser.DeferNode:
		selected, err := s.selectNode(node.Action)
		if err != nil {
			return nil, err
		}
		if len(selected) == 1 {
			node.Action = selected[0]
		}
	case *parser.AssignmentNode:
		var err error
		node.Value, err = s.selectExpression(node.Value)
		if err != nil {
			return nil, err
		}
	case *parser.MultiDeclarationNode:
		var err error
		node.Value, err = s.selectExpression(node.Value)
		if err != nil {
			return nil, err
		}
	}
	return []parser.Node{node}, nil
}

func (s *stageSelector) selectExpression(expression parser.ExpressionNode) (parser.ExpressionNode, error) {
	if expression == nil {
		return nil, nil
	}
	if when, ok := expression.(*parser.WhenNode); ok {
		value, resolved, err := s.selectWhenValue(when)
		if err != nil || !resolved {
			return expression, err
		}
		return s.selectExpression(value.(parser.ExpressionNode))
	}
	var err error
	switch node := expression.(type) {
	case *parser.BlockNode:
		_, err = s.selectNode(node)
	case *parser.IfNode:
		_, err = s.selectNode(node)
	case *parser.MatchNode:
		_, err = s.selectNode(node)
	case *parser.UnaryOpNode:
		node.Operand, err = s.selectExpression(node.Operand)
	case *parser.BinaryOpNode:
		node.Operand1, err = s.selectExpression(node.Operand1)
		if err == nil {
			node.Operand2, err = s.selectExpression(node.Operand2)
		}
	case *parser.FunctionCallNode:
		node.Callee, err = s.selectExpression(node.Callee)
		for index := range node.Args {
			if err != nil {
				break
			}
			node.Args[index], err = s.selectExpression(node.Args[index])
		}
	case *parser.IndexExprNode:
		node.Subject, err = s.selectExpression(node.Subject)
		if err == nil {
			node.Index, err = s.selectExpression(node.Index)
		}
	case *parser.SliceExprNode:
		node.Subject, err = s.selectExpression(node.Subject)
		if err == nil {
			node.Start, err = s.selectExpression(node.Start)
		}
		if err == nil {
			node.End, err = s.selectExpression(node.End)
		}
	case *parser.CastNode:
		node.ToType, err = s.selectType(node.ToType)
		if err == nil {
			node.Operand, err = s.selectExpression(node.Operand)
		}
	case *parser.StructLiteralNode:
		for index := range node.Fields {
			if err != nil {
				break
			}
			node.Fields[index].R, err = s.selectExpression(node.Fields[index].R)
		}
	case *parser.SliceLiteralNode:
		for index := range node.Elements {
			if err != nil {
				break
			}
			node.Elements[index], err = s.selectExpression(node.Elements[index])
		}
		if err == nil {
			node.RepeatValue, err = s.selectExpression(node.RepeatValue)
		}
		if err == nil {
			node.RepeatAmount, err = s.selectExpression(node.RepeatAmount)
		}
	}
	return expression, err
}

func (s *stageSelector) selectType(typeNode parser.TypeNode) (parser.TypeNode, error) {
	if typeNode == nil {
		return nil, nil
	}
	if when, ok := typeNode.(*parser.WhenNode); ok {
		value, resolved, err := s.selectWhenValue(when)
		if err != nil || !resolved {
			return typeNode, err
		}
		return s.selectType(value.(parser.TypeNode))
	}
	var err error
	switch node := typeNode.(type) {
	case *parser.NamedTypeNode:
		for index := range node.TypeArguments {
			if err != nil {
				break
			}
			node.TypeArguments[index], err = s.selectType(node.TypeArguments[index])
		}
	case *parser.ReprTypeNode:
		node.Operand, err = s.selectType(node.Operand)
	case *parser.PointerTypeNode:
		node.BaseType, err = s.selectType(node.BaseType)
	case *parser.SliceTypeNode:
		node.ElementType, err = s.selectType(node.ElementType)
	case *parser.ArrayTypeNode:
		node.ElementType, err = s.selectType(node.ElementType)
		if err == nil {
			node.Length, err = s.selectExpression(node.Length)
			node.Length = s.inlineStageExpression(node.Length)
		}
	case *parser.DynTypeNode:
		node.TraitType, err = s.selectType(node.TraitType)
	case *parser.FunctionTypeNode:
		for index := range node.Parameters {
			if err != nil {
				break
			}
			node.Parameters[index], err = s.selectType(node.Parameters[index])
		}
		if err == nil {
			node.ReturnType, err = s.selectType(node.ReturnType)
		}
	case *parser.MultipleReturnTypeNode:
		for index := range node.Types {
			if err != nil {
				break
			}
			node.Types[index], err = s.selectType(node.Types[index])
		}
	case *parser.StructTypeNode:
		for index := range node.Fields {
			if err != nil {
				break
			}
			node.Fields[index].Type, err = s.selectType(node.Fields[index].Type)
		}
	case *parser.FlagsTypeNode:
		node.Underlying, err = s.selectType(node.Underlying)
	case *parser.UnionTypeNode:
		node.TagType, err = s.selectType(node.TagType)
		for index := range node.Fields {
			if err != nil {
				break
			}
			node.Fields[index].Type, err = s.selectType(node.Fields[index].Type)
		}
		for variantIndex := range node.Variants {
			for fieldIndex := range node.Variants[variantIndex].Fields {
				if err != nil {
					break
				}
				field := &node.Variants[variantIndex].Fields[fieldIndex]
				field.Type, err = s.selectType(field.Type)
			}
		}
	}
	return typeNode, err
}

func (s *stageSelector) inlineStageExpression(expression parser.ExpressionNode) parser.ExpressionNode {
	switch node := expression.(type) {
	case *parser.IdentifierNode:
		if value, found := s.bindings[node.Name]; found &&
			(value.kind == stageBool || value.kind == stageInteger || value.kind == stageFloat) {
			return stageLiteral(value, node.Loc)
		}
	case *parser.UnaryOpNode:
		node.Operand = s.inlineStageExpression(node.Operand)
	case *parser.BinaryOpNode:
		node.Operand1 = s.inlineStageExpression(node.Operand1)
		node.Operand2 = s.inlineStageExpression(node.Operand2)
	case *parser.CastNode:
		node.Operand = s.inlineStageExpression(node.Operand)
	}
	return expression
}

func (s *stageSelector) selectWhen(node *parser.WhenNode) ([]parser.Node, bool, error) {
	for _, branch := range node.Branches {
		value, resolved, err := s.evaluate(branch.Condition)
		if err != nil {
			return nil, false, err
		}
		if !resolved {
			s.needsIR = true
			s.unresolved[branch.Condition] = struct{}{}
			return nil, false, nil
		}
		if value.kind != stageBool {
			return nil, false, shared.NewError(branch.Condition.GetLoc(), "compile-time condition must be boolean")
		}
		if value.boolean {
			body, err := s.selectBody(branch.Body)
			return body, true, err
		}
	}
	body, err := s.selectBody(node.ElseBody)
	return body, true, err
}

func (s *stageSelector) selectWhenValue(node *parser.WhenNode) (parser.Node, bool, error) {
	for _, branch := range node.Branches {
		condition, resolved, err := s.evaluate(branch.Condition)
		if err != nil || !resolved {
			if err == nil {
				s.needsIR = true
				s.unresolved[branch.Condition] = struct{}{}
			}
			return nil, resolved, err
		}
		if condition.kind != stageBool {
			return nil, false, shared.NewError(branch.Condition.GetLoc(), "compile-time condition must be boolean")
		}
		if condition.boolean {
			if directive, ok := branch.Value.(*parser.CompilerDirectiveNode); ok {
				return nil, true, s.evaluateDirective(directive)
			}
			if nested, ok := branch.Value.(*parser.WhenNode); ok {
				return s.selectWhenValue(nested)
			}
			return branch.Value, true, nil
		}
	}
	if directive, ok := node.ElseValue.(*parser.CompilerDirectiveNode); ok {
		return nil, true, s.evaluateDirective(directive)
	}
	if nested, ok := node.ElseValue.(*parser.WhenNode); ok {
		return s.selectWhenValue(nested)
	}
	return node.ElseValue, true, nil
}

func (s *stageSelector) selectLinkItems(items []parser.LinkItemNode) ([]attributes.Link, bool, error) {
	var links []attributes.Link
	for _, item := range items {
		if item.Link != nil {
			links = append(links, *item.Link)
			continue
		}
		if item.Directive != nil {
			if err := s.evaluateDirective(item.Directive); err != nil {
				return nil, false, err
			}
			if _, unresolved := s.unresolved[item.Directive.Condition]; unresolved {
				return nil, false, nil
			}
			continue
		}
		if item.When != nil {
			selected, resolved, err := s.selectLinkWhen(item.When)
			if err != nil || !resolved {
				return nil, resolved, err
			}
			links = append(links, selected...)
		}
	}
	return links, true, nil
}

func (s *stageSelector) selectLinkWhen(node *parser.LinkWhenNode) ([]attributes.Link, bool, error) {
	for _, branch := range node.Branches {
		condition, resolved, err := s.evaluate(branch.Condition)
		if err != nil || !resolved {
			if err == nil {
				s.needsIR = true
				s.unresolved[branch.Condition] = struct{}{}
			}
			return nil, resolved, err
		}
		if condition.kind != stageBool {
			return nil, false, shared.NewError(branch.Condition.GetLoc(), "@link condition must be boolean")
		}
		if condition.boolean {
			return s.selectLinkItems(branch.Items)
		}
	}
	return s.selectLinkItems(node.ElseItems)
}

func (s *stageSelector) evaluateDirective(node *parser.CompilerDirectiveNode) error {
	if node.Name == "compiler_error" {
		return shared.NewError(node.NameLoc, "%s", node.Message)
	}
	condition, resolved, err := s.evaluate(node.Condition)
	if err != nil {
		return err
	}
	if !resolved {
		s.needsIR = true
		s.unresolved[node.Condition] = struct{}{}
		return nil
	}
	if condition.kind != stageBool || !condition.boolean {
		return shared.NewError(node.Loc, "compiler assertion failed: %s", node.Message)
	}
	return nil
}

func stageLiteral(value stageValue, loc shared.Location) parser.ExpressionNode {
	if value.kind == stageBool {
		text := string(tokeniser.KeywordFalse)
		if value.boolean {
			text = string(tokeniser.KeywordTrue)
		}
		return &parser.BoolLiteralNode{Value: text, Loc: loc}
	}
	if value.kind == stageFloat {
		return &parser.FloatLiteralNode{Value: value.floating, Loc: loc}
	}
	return &parser.IntegerLiteralNode{Value: value.integer.String(), Loc: loc}
}

func stageTypeNode(value stageValue, loc shared.Location) parser.TypeNode {
	primitive, ok := types.Underlying(value.typ).(types.PrimitiveType)
	if !ok || primitive == types.PrimitiveVoid {
		return nil
	}
	return &parser.NamedTypeNode{Name: string(primitive), Loc: loc}
}

func (s *stageSelector) evaluate(node parser.ExpressionNode) (stageValue, bool, error) {
	if value, ok := s.evaluated[node]; ok {
		return value, true, nil
	}
	switch node := node.(type) {
	case *parser.BoolLiteralNode:
		return stageValue{kind: stageBool, boolean: node.Value == string(tokeniser.KeywordTrue)}, true, nil
	case *parser.IntegerLiteralNode:
		value, ok := new(big.Int).SetString(node.Value, 0)
		if !ok {
			return stageValue{}, false, shared.NewError(node.Loc, "invalid compile-time integer")
		}
		return stageValue{kind: stageInteger, integer: value}, true, nil
	case *parser.FloatLiteralNode:
		return stageValue{kind: stageFloat, floating: node.Value}, true, nil
	case *parser.EnumLiteralNode:
		return stageValue{kind: stageEnumLiteral, name: node.Variant}, true, nil
	case *parser.IdentifierNode:
		if value, ok := s.targetValue(node.Name); ok {
			return value, true, nil
		}
		value, ok := s.bindings[node.Name]
		return value, ok, nil
	case *parser.UnaryOpNode:
		operand, resolved, err := s.evaluate(node.Operand)
		if err != nil || !resolved {
			return stageValue{}, resolved, err
		}
		if node.Op == parser.UnaryOpLogicalNot && operand.kind == stageBool {
			operand.boolean = !operand.boolean
			return operand, true, nil
		}
		if node.Op == parser.UnaryOpNegate && operand.kind == stageInteger {
			return stageValue{kind: stageInteger, integer: new(big.Int).Neg(operand.integer)}, true, nil
		}
		if node.Op == parser.UnaryOpNegate && operand.kind == stageFloat {
			value := operand.floating
			if strings.HasPrefix(value, "-") {
				value = strings.TrimPrefix(value, "-")
			} else {
				value = "-" + value
			}
			return stageValue{kind: stageFloat, floating: value}, true, nil
		}
		return stageValue{}, false, shared.NewError(node.Loc, "invalid compile-time unary operation")
	case *parser.BinaryOpNode:
		return s.evaluateBinary(node)
	case *parser.FunctionCallNode:
		return stageValue{}, false, nil
	default:
		return stageValue{}, false, nil
	}
}

func (s *stageSelector) evaluateBinary(node *parser.BinaryOpNode) (stageValue, bool, error) {
	left, resolved, err := s.evaluate(node.Operand1)
	if err != nil || !resolved {
		return stageValue{}, resolved, err
	}
	right, resolved, err := s.evaluate(node.Operand2)
	if err != nil || !resolved {
		return stageValue{}, resolved, err
	}
	switch node.Op {
	case parser.BinaryOpLogicalAnd, parser.BinaryOpLogicalOr:
		if left.kind != stageBool || right.kind != stageBool {
			return stageValue{}, false, shared.NewError(node.Loc, "logical compile-time operation requires booleans")
		}
		value := left.boolean && right.boolean
		if node.Op == parser.BinaryOpLogicalOr {
			value = left.boolean || right.boolean
		}
		return stageValue{kind: stageBool, boolean: value}, true, nil
	case parser.BinaryOpEqual, parser.BinaryOpNotEqual:
		equal := stageEqual(left, right)
		if node.Op == parser.BinaryOpNotEqual {
			equal = !equal
		}
		return stageValue{kind: stageBool, boolean: equal}, true, nil
	case parser.BinaryOpAdd, parser.BinaryOpSubtract, parser.BinaryOpMultiply, parser.BinaryOpDivide, parser.BinaryOpModulo:
		if left.kind != stageInteger || right.kind != stageInteger {
			return stageValue{}, false, shared.NewError(node.Loc, "compile-time arithmetic requires integers")
		}
		value := new(big.Int)
		switch node.Op {
		case parser.BinaryOpAdd:
			value.Add(left.integer, right.integer)
		case parser.BinaryOpSubtract:
			value.Sub(left.integer, right.integer)
		case parser.BinaryOpMultiply:
			value.Mul(left.integer, right.integer)
		case parser.BinaryOpDivide:
			if right.integer.Sign() == 0 {
				return stageValue{}, false, shared.NewError(node.Loc, "division by zero")
			}
			value.Quo(left.integer, right.integer)
		case parser.BinaryOpModulo:
			if right.integer.Sign() == 0 {
				return stageValue{}, false, shared.NewError(node.Loc, "division by zero")
			}
			value.Rem(left.integer, right.integer)
		}
		return stageValue{kind: stageInteger, integer: value}, true, nil
	case parser.BinaryOpLess, parser.BinaryOpLessEqual, parser.BinaryOpGreater, parser.BinaryOpGreaterEqual:
		if left.kind != stageInteger || right.kind != stageInteger {
			return stageValue{}, false, shared.NewError(node.Loc, "compile-time comparison requires integers")
		}
		comparison := left.integer.Cmp(right.integer)
		value := comparison < 0
		if node.Op == parser.BinaryOpLessEqual {
			value = comparison <= 0
		}
		if node.Op == parser.BinaryOpGreater {
			value = comparison > 0
		}
		if node.Op == parser.BinaryOpGreaterEqual {
			value = comparison >= 0
		}
		return stageValue{kind: stageBool, boolean: value}, true, nil
	}
	return stageValue{}, false, nil
}

func stageEqual(left, right stageValue) bool {
	if left.kind == stageEnum && right.kind == stageEnumLiteral {
		return left.name == right.name
	}
	if right.kind == stageEnum && left.kind == stageEnumLiteral {
		return right.name == left.name
	}
	if left.kind != right.kind {
		return false
	}
	if left.kind == stageBool {
		return left.boolean == right.boolean
	}
	if left.kind == stageInteger {
		return left.integer.Cmp(right.integer) == 0
	}
	if left.kind == stageFloat {
		return left.floating == right.floating
	}
	return left.domain == right.domain && left.name == right.name
}

func (s *stageSelector) targetValue(name string) (stageValue, bool) {
	triple := qktarget.EffectiveTriple(s.config.TargetTriple)
	lower := strings.ToLower(triple)
	switch name {
	case "PointerBits":
		bits, _ := qktarget.PointerBits(triple)
		return stageValue{kind: stageInteger, integer: big.NewInt(int64(bits))}, true
	case "CCharSigned":
		return stageValue{kind: stageBool, boolean: qktarget.CCharSigned(triple)}, true
	case "ReleaseMode":
		value := "Debug"
		if s.config.ReleaseMode == StageRelease {
			value = "Release"
		}
		return stageValue{kind: stageEnum, domain: "ReleaseMode", name: value}, true
	case "OS":
		value := "Unknown"
		switch {
		case strings.Contains(lower, "windows"), strings.Contains(lower, "mingw"), strings.Contains(lower, "msvc"):
			value = "Windows"
		case strings.Contains(lower, "linux"):
			value = "Linux"
		case strings.Contains(lower, "darwin"), strings.Contains(lower, "apple"):
			value = "MacOS"
		case strings.Contains(lower, "freebsd"):
			value = "FreeBSD"
		case strings.Contains(lower, "openbsd"):
			value = "OpenBSD"
		case strings.Contains(lower, "netbsd"):
			value = "NetBSD"
		case strings.Contains(lower, "dragonfly"):
			value = "DragonFly"
		case strings.Contains(lower, "wasi"):
			value = "WASI"
		}
		return stageValue{kind: stageEnum, domain: "OS", name: value}, true
	case "Arch":
		value := map[string]string{"386": "X86", "x86_64": "X86_64", "amd64": "X86_64", "aarch64": "AArch64", "arm64": "AArch64", "wasm32": "Wasm32", "wasm64": "Wasm64"}[qktarget.Arch(triple)]
		return stageValue{kind: stageEnum, domain: "Arch", name: value}, true
	case "Environment":
		value := "Unknown"
		if strings.Contains(lower, "msvc") {
			value = "MSVC"
		} else if strings.Contains(lower, "musl") {
			value = "Musl"
		} else if strings.Contains(lower, "gnu") || strings.Contains(lower, "mingw") {
			value = "GNU"
		}
		return stageValue{kind: stageEnum, domain: "Environment", name: value}, true
	case "TargetHasLibc", "TargetHasFilesystem", "TargetHasEnvironment", "TargetHasProcessExit":
		os, _ := s.targetValue("OS")
		native := os.name == "Windows" || os.name == "Linux" || os.name == "MacOS" || os.name == "FreeBSD" || os.name == "OpenBSD" || os.name == "NetBSD" || os.name == "DragonFly"
		return stageValue{kind: stageBool, boolean: native && !(name == "TargetHasLibc" && s.config.NoLibc)}, true
	}
	return stageValue{}, false
}

func (s *stageSelector) evaluateUnresolved(root *parser.RootNode) error {
	staged := &parser.RootNode{Loc: root.Loc}
	reachableFunctions := s.reachableStageFunctions(root)
	hasModule := false
	for _, node := range root.Body {
		switch node := node.(type) {
		case *parser.WhenNode, *parser.CompilerDirectiveNode:
			continue
		case *parser.ImportNode:
			if s.environment != nil {
				staged.Body = append(staged.Body, node)
			}
		case *parser.ModuleNode:
			if hasModule {
				continue
			}
			hasModule = true
			copy := *node
			copy.Attributes = nil
			staged.Body = append(staged.Body, &copy)
		case *parser.FunctionDefNode:
			if reachableFunctions[node] {
				staged.Body = append(staged.Body, node)
			}
		default:
			staged.Body = append(staged.Body, node)
		}
	}

	names := make(map[parser.ExpressionNode]string, len(s.unresolved))
	for expression := range s.unresolved {
		name := fmt.Sprintf("__qk_stage_%d", s.syntheticID)
		s.syntheticID++
		names[expression] = name
		staged.Body = append(staged.Body, &parser.DeclarationNode{
			Name: name, NameLoc: expression.GetLoc(), Value: expression,
			Comptime: true, Loc: expression.GetLoc(),
		})
	}

	modules, order, err := s.stageModules(staged)
	if err != nil {
		return err
	}
	analyser := sema.NewAnalyser()
	if errors, _ := RunSemanticPipeline(modules, analyser, order, false, false); len(errors) != 0 {
		return errors[0]
	}
	PropagateSpecializationDemands(modules, order)
	irModules, irErrors := GenerateIRModules(modules, s.module, order, false, false)
	if len(irErrors) != 0 {
		return irErrors[0]
	}
	templates := make(map[string][]ir.GenericTemplate, len(order))
	for _, name := range order {
		templates[name] = GenerateModuleGenericTemplateIR(modules[name])
	}
	specializations, err := ExtractGenericSpecializations(irModules, templates, analyser.ModuleInterfaces())
	if err != nil {
		return err
	}
	allIR := make([]*ir.Module, 0, len(order)+1)
	for _, name := range order {
		if moduleIR := irModules[name]; moduleIR != nil {
			allIR = append(allIR, moduleIR)
		}
	}
	if specializations != nil {
		allIR = append(allIR, specializations)
	}
	evaluator := ir.NewEvaluator(allIR...)
	moduleIR := irModules[s.module]
	if moduleIR.Initializer != "" {
		if _, err := evaluator.Run(moduleIR.Initializer); err != nil {
			var unavailable *ir.CompileTimeUnavailableError
			if errors.As(err, &unavailable) && unavailable.SourceLoc.FilePath != "" {
				return stageUnavailableDiagnostic(unavailable)
			}
			if len(names) == 1 {
				for expression := range names {
					return shared.NewError(expression.GetLoc(), err.Error())
				}
			}
			return err
		}
	}
	for expression, name := range names {
		value, ok := findEvaluatedGlobal(evaluator.Globals, name)
		if !ok {
			return shared.NewError(expression.GetLoc(), "compile-time expression did not produce a value")
		}
		s.evaluated[expression] = stageValueFromIR(value)
	}
	for _, raw := range root.Body {
		declaration, ok := raw.(*parser.DeclarationNode)
		if !ok || !declaration.Comptime {
			continue
		}
		if value, ok := findEvaluatedGlobal(evaluator.Globals, declaration.Name); ok {
			stagedValue := stageValueFromIR(value)
			s.bindings[declaration.Name] = stagedValue
			s.evaluated[declaration.Value] = stagedValue
		}
	}
	return nil
}

func stageUnavailableDiagnostic(unavailable *ir.CompileTimeUnavailableError) error {
	if len(unavailable.CallStack) == 0 {
		return shared.NewError(unavailable.SourceLoc, unavailable.Error())
	}
	root := unavailable.CallStack[len(unavailable.CallStack)-1]
	cause := ir.SourceOrigin{Name: unavailable.SourceName, Loc: unavailable.SourceLoc}
	if cause.Loc.FilePath != root.Loc.FilePath {
		for _, call := range unavailable.CallStack[:len(unavailable.CallStack)-1] {
			if call.Loc.FilePath == root.Loc.FilePath {
				cause = call
				break
			}
		}
	}
	if sameStageLocation(root.Loc, cause.Loc) {
		return shared.NewError(cause.Loc, "%s is not available in a compile-time context", cause.Name)
	}
	return shared.NewError(root.Loc, "%s cannot be evaluated at compile time", root.Name).
		WithNote(cause.Loc, "evaluation reaches %s, which is not available in a compile-time context", cause.Name)
}

func sameStageLocation(left, right shared.Location) bool {
	return left.FilePath == right.FilePath && left.Offset == right.Offset && left.EndOffset == right.EndOffset
}

func (s *stageSelector) stageModules(staged *parser.RootNode) (map[string]*ModuleInfo, []string, error) {
	modules := make(map[string]*ModuleInfo)
	var order []string
	if s.environment != nil {
		for _, name := range s.environment.Order {
			dependency := s.environment.Modules[name]
			if dependency == nil || name == s.module {
				continue
			}
			modules[name] = &ModuleInfo{
				Path: name, Name: dependency.Name,
				Imports:                append([]string(nil), dependency.Imports...),
				Root:                   parser.CloneSyntax(dependency.Root).(*parser.RootNode),
				TrustedStandardLibrary: dependency.TrustedStandardLibrary,
			}
			order = append(order, name)
		}
	}
	trusted := s.environment != nil && s.environment.TrustedStandardLibrary
	currentRoot := parser.CloneSyntax(staged).(*parser.RootNode)
	partial, err := CollectModuleInfo(currentRoot, trusted)
	if err != nil {
		return nil, nil, err
	}
	modules[s.module] = &ModuleInfo{
		Path: s.module, Name: partial.Name, Imports: partial.Imports, Root: currentRoot,
		TrustedStandardLibrary: trusted,
	}
	order = append(order, s.module)
	return modules, order, nil
}

func (s *stageSelector) reachableStageFunctions(root *parser.RootNode) map[*parser.FunctionDefNode]bool {
	definitions := make(map[string][]*parser.FunctionDefNode)
	for _, node := range root.Body {
		if function, ok := node.(*parser.FunctionDefNode); ok {
			definitions[function.Name] = append(definitions[function.Name], function)
		}
	}
	names := make(map[string]bool)
	for expression := range s.unresolved {
		collectStageCalls(expression, names)
	}
	result := make(map[*parser.FunctionDefNode]bool)
	for progress := true; progress; {
		progress = false
		for name := range names {
			for _, function := range definitions[name] {
				if result[function] {
					continue
				}
				result[function] = true
				collectStageCalls(function.Body, names)
				progress = true
			}
		}
	}
	return result
}

func collectStageCalls(node parser.Node, names map[string]bool) {
	if node == nil {
		return
	}
	switch node := node.(type) {
	case *parser.FunctionCallNode:
		if identifier, ok := node.Callee.(*parser.IdentifierNode); ok {
			names[identifier.Name] = true
		}
		collectStageCalls(node.Callee, names)
		for _, argument := range node.Args {
			collectStageCalls(argument, names)
		}
	case *parser.BlockNode:
		for _, child := range node.Body {
			collectStageCalls(child, names)
		}
	case *parser.DeclarationNode:
		collectStageCalls(node.Value, names)
	case *parser.MultiDeclarationNode:
		collectStageCalls(node.Value, names)
	case *parser.AssignmentNode:
		collectStageCalls(node.Value, names)
	case *parser.ControlKeywordNode:
		collectStageCalls(node.ReturnValue, names)
		for _, value := range node.ReturnValues {
			collectStageCalls(value, names)
		}
	case *parser.DeferNode:
		collectStageCalls(node.Action, names)
	case *parser.IfNode:
		collectStageCalls(node.IfBranch.Condition, names)
		collectStageCalls(node.IfBranch.Node, names)
		for _, branch := range node.ElseIfBranches {
			collectStageCalls(branch.Condition, names)
			collectStageCalls(branch.Node, names)
		}
		if node.ElseBranch != nil {
			collectStageCalls(node.ElseBranch, names)
		}
	case *parser.ForNode:
		for _, child := range node.ExprsOrStmts {
			collectStageCalls(child, names)
		}
		collectStageCalls(node.Body, names)
	case *parser.RangeForNode:
		collectStageCalls(node.Start, names)
		collectStageCalls(node.End, names)
		collectStageCalls(node.Body, names)
	case *parser.ForEachNode:
		collectStageCalls(node.Iterable, names)
		collectStageCalls(node.Body, names)
	case *parser.MatchNode:
		for _, subject := range node.Subjects {
			collectStageCalls(subject, names)
		}
		for _, arm := range node.Arms {
			collectStageCalls(arm.Guard, names)
			collectStageCalls(arm.Body, names)
		}
	case *parser.UnaryOpNode:
		collectStageCalls(node.Operand, names)
	case *parser.BinaryOpNode:
		collectStageCalls(node.Operand1, names)
		collectStageCalls(node.Operand2, names)
	case *parser.IndexExprNode:
		collectStageCalls(node.Subject, names)
		collectStageCalls(node.Index, names)
	case *parser.SliceExprNode:
		collectStageCalls(node.Subject, names)
		collectStageCalls(node.Start, names)
		collectStageCalls(node.End, names)
	case *parser.CastNode:
		collectStageCalls(node.Operand, names)
	case *parser.StructLiteralNode:
		for _, field := range node.Fields {
			collectStageCalls(field.R, names)
		}
	case *parser.SliceLiteralNode:
		for _, element := range node.Elements {
			collectStageCalls(element, names)
		}
		collectStageCalls(node.RepeatValue, names)
		collectStageCalls(node.RepeatAmount, names)
	}
}

func findEvaluatedGlobal(globals map[string]ir.EvalValue, sourceName string) (ir.EvalValue, bool) {
	suffix := "_global_" + sourceName
	for name, value := range globals {
		if strings.HasSuffix(name, suffix) {
			return value, true
		}
	}
	return ir.EvalValue{}, false
}

func stageValueFromIR(value ir.EvalValue) stageValue {
	if value.Float != nil {
		return stageValue{kind: stageFloat, floating: strconv.FormatFloat(*value.Float, 'g', -1, 64), typ: value.Type}
	}
	if value.Integer == nil {
		return stageValue{kind: stageBool, boolean: value.Boolean, typ: value.Type}
	}
	return stageValue{kind: stageInteger, integer: new(big.Int).Set(value.Integer), typ: value.Type}
}
