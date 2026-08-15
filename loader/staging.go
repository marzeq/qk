package loader

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/marzeq/qk/attributes"
	"github.com/marzeq/qk/codegen/irgen"
	"github.com/marzeq/qk/ir"
	"github.com/marzeq/qk/parser"
	"github.com/marzeq/qk/sema"
	"github.com/marzeq/qk/shared"
	qktarget "github.com/marzeq/qk/target"
	"github.com/marzeq/qk/tokeniser"
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

type stageKind uint8

const (
	stageBool stageKind = iota
	stageInteger
	stageEnum
	stageEnumLiteral
)

type stageValue struct {
	kind    stageKind
	boolean bool
	integer *big.Int
	domain  string
	name    string
}

type stageSelector struct {
	config      StageConfig
	module      string
	bindings    map[string]stageValue
	evaluated   map[parser.ExpressionNode]stageValue
	unresolved  map[parser.ExpressionNode]struct{}
	needsIR     bool
	syntheticID int
}

// SelectCompileTime lowers unresolved staged expressions through the ordinary
// semantic and IR pipelines, evaluates them, and removes parsed when nodes.
func SelectCompileTime(root *parser.RootNode, module string, config StageConfig) error {
	selector := &stageSelector{
		config: config, module: module,
		bindings: make(map[string]stageValue), evaluated: make(map[parser.ExpressionNode]stageValue),
		unresolved: make(map[parser.ExpressionNode]struct{}),
	}
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
			} else {
				s.needsIR = true
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
	case *parser.PointerTypeNode:
		node.BaseType, err = s.selectType(node.BaseType)
	case *parser.SliceTypeNode:
		node.ElementType, err = s.selectType(node.ElementType)
	case *parser.ArrayTypeNode:
		node.ElementType, err = s.selectType(node.ElementType)
		if err == nil {
			node.Length, err = s.selectExpression(node.Length)
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
	}
	return typeNode, err
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
	return &parser.IntegerLiteralNode{Value: value.integer.String(), Loc: loc}
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
	for _, node := range root.Body {
		switch node := node.(type) {
		case *parser.WhenNode, *parser.CompilerDirectiveNode, *parser.ImportNode:
			continue
		case *parser.ModuleNode:
			copy := *node
			copy.Attributes = nil
			staged.Body = append(staged.Body, &copy)
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

	analyser := sema.NewAnalyser()
	info := &ModuleInfo{Path: s.module, Name: s.module, Root: staged}
	if errors, _ := RunSemanticModule(info, analyser, false, false); len(errors) != 0 {
		return errors[0]
	}
	moduleIR := (&irgen.Generator{ModuleName: s.module, MainModule: s.module}).Generate(staged)
	evaluator := ir.NewEvaluator(moduleIR)
	if moduleIR.Initializer != "" {
		if _, err := evaluator.Run(moduleIR.Initializer); err != nil {
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
	if value.Integer == nil {
		return stageValue{kind: stageBool, boolean: value.Boolean}
	}
	return stageValue{kind: stageInteger, integer: new(big.Int).Set(value.Integer)}
}
