package diff

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
	"nacos-config-sync-tools/internal/rules"
)

type ChangeKind string

const (
	ChangeAdd    ChangeKind = "add"
	ChangeModify ChangeKind = "modify"
	ChangeDelete ChangeKind = "delete"
)

type Change struct {
	Path        string              `json:"path"`
	Kind        ChangeKind          `json:"kind"`
	SourceValue any                 `json:"sourceValue,omitempty"`
	TargetValue any                 `json:"targetValue,omitempty"`
	ListItems   []ListItemChange    `json:"listItems,omitempty"`
	Sensitive   rules.SensitiveKind `json:"sensitive,omitempty"`
}

type ListItemChange struct {
	Key  string `json:"key"`
	Side string `json:"side"`
}

type Warning struct {
	Path      string              `json:"path"`
	Kind      rules.SensitiveKind `json:"kind"`
	Message   string              `json:"message"`
	SourceVal any                 `json:"sourceValue,omitempty"`
	TargetVal any                 `json:"targetValue,omitempty"`
}

type Result struct {
	Changes  []Change  `json:"changes"`
	Warnings []Warning `json:"warnings"`
}

func (r Result) HasChange(path string) bool {
	for _, change := range r.Changes {
		if change.Path == path {
			return true
		}
	}
	return false
}

func (r Result) HasWarning(path string) bool {
	for _, warning := range r.Warnings {
		if warning.Path == path {
			return true
		}
	}
	return false
}

func CompareYAML(dataID, source, target string, ruleSet rules.RuleSet) (Result, error) {
	sourceValue, err := parseYAML(source)
	if err != nil {
		return Result{}, fmt.Errorf("parse source YAML: %w", err)
	}
	targetValue, err := parseYAML(target)
	if err != nil {
		return Result{}, fmt.Errorf("parse target YAML: %w", err)
	}

	sourceFlat := map[string]any{}
	targetFlat := map[string]any{}
	flatten("", sourceValue, sourceFlat)
	flatten("", targetValue, targetFlat)

	keys := uniqueSortedKeys(sourceFlat, targetFlat)
	result := Result{}
	for _, path := range keys {
		sourceVal, sourceOK := sourceFlat[path]
		targetVal, targetOK := targetFlat[path]
		if ruleSet.ShouldIgnore(dataID, path) {
			continue
		}

		sensitive := firstSensitive(sourceVal, targetVal)

		switch {
		case sourceOK && !targetOK:
			sourceVal, targetVal = filterIgnoredListValues(dataID, path, sourceVal, nil, ruleSet)
			if isEmptyList(sourceVal) {
				continue
			}
			result.Changes = append(result.Changes, Change{Path: path, Kind: ChangeAdd, SourceValue: sourceVal, Sensitive: sensitive, ListItems: listItemChanges(sourceVal, nil)})
			result.addSensitiveWarning(path, sensitive, sourceVal, targetVal)
		case !sourceOK && targetOK:
			sourceVal, targetVal = filterIgnoredListValues(dataID, path, nil, targetVal, ruleSet)
			if isEmptyList(targetVal) {
				continue
			}
			result.Changes = append(result.Changes, Change{Path: path, Kind: ChangeDelete, TargetValue: targetVal, Sensitive: sensitive, ListItems: listItemChanges(nil, targetVal)})
			result.addSensitiveWarning(path, sensitive, sourceVal, targetVal)
		case sourceOK && targetOK && !reflect.DeepEqual(sourceVal, targetVal):
			sourceVal, targetVal = filterIgnoredListValues(dataID, path, sourceVal, targetVal, ruleSet)
			if reflect.DeepEqual(sourceVal, targetVal) {
				continue
			}
			result.Changes = append(result.Changes, Change{Path: path, Kind: ChangeModify, SourceValue: sourceVal, TargetValue: targetVal, Sensitive: sensitive, ListItems: listItemChanges(sourceVal, targetVal)})
			result.addSensitiveWarning(path, sensitive, sourceVal, targetVal)
		}
	}
	return result, nil
}

func filterIgnoredListValues(dataID, path string, source, target any, ruleSet rules.RuleSet) (any, any) {
	sourceValues, sourceOK := source.([]any)
	targetValues, targetOK := target.([]any)
	if !sourceOK && !targetOK {
		return source, target
	}
	if sourceOK {
		source = filterIgnoredListItems(dataID, path, sourceValues, ruleSet)
	}
	if targetOK {
		target = filterIgnoredListItems(dataID, path, targetValues, ruleSet)
	}
	return source, target
}

func filterIgnoredListItems(dataID, path string, values []any, ruleSet rules.RuleSet) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		if ruleSet.ShouldIgnore(dataID, ListItemPath(path, listValueKey(value))) {
			continue
		}
		out = append(out, value)
	}
	return out
}

func isEmptyList(value any) bool {
	values, ok := value.([]any)
	return ok && len(values) == 0
}

func ListItemPath(path, itemKey string) string {
	return path + "[" + itemKey + "]"
}

func listItemChanges(source, target any) []ListItemChange {
	sourceValues, sourceOK := source.([]any)
	targetValues, targetOK := target.([]any)
	if !sourceOK && !targetOK {
		return nil
	}
	sourceCounts := listValueCounts(sourceValues)
	targetCounts := listValueCounts(targetValues)
	changes := []ListItemChange{}
	for key, sourceCount := range sourceCounts {
		if sourceCount <= targetCounts[key] {
			continue
		}
		for i := 0; i < sourceCount-targetCounts[key]; i++ {
			changes = append(changes, ListItemChange{Key: key, Side: "source"})
		}
	}
	for key, targetCount := range targetCounts {
		if targetCount <= sourceCounts[key] {
			continue
		}
		for i := 0; i < targetCount-sourceCounts[key]; i++ {
			changes = append(changes, ListItemChange{Key: key, Side: "target"})
		}
	}
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Side == changes[j].Side {
			return changes[i].Key < changes[j].Key
		}
		return changes[i].Side < changes[j].Side
	})
	return changes
}

func listValueCounts(values []any) map[string]int {
	counts := map[string]int{}
	for _, value := range values {
		counts[listValueKey(value)]++
	}
	return counts
}

func listValueKey(value any) string {
	if str, ok := value.(string); ok {
		return str
	}
	if key, ok := identityValueKey(value); ok {
		return key
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(data)
}

func (r *Result) addSensitiveWarning(path string, kind rules.SensitiveKind, sourceVal, targetVal any) {
	if kind == rules.SensitiveNone {
		return
	}
	r.Warnings = append(r.Warnings, Warning{
		Path:      path,
		Kind:      kind,
		Message:   "疑似环境相关配置，请确认是否需要同步",
		SourceVal: sourceVal,
		TargetVal: targetVal,
	})
}

func MergeYAML(dataID, source, target string, ruleSet rules.RuleSet) (string, error) {
	if strings.TrimSpace(target) == "" {
		return source, nil
	}
	result, err := CompareYAML(dataID, source, target, ruleSet)
	if err != nil {
		return "", err
	}
	merged := target
	for _, change := range result.Changes {
		merged, err = ApplySourcePath(source, merged, change.Path)
		if err != nil {
			return "", err
		}
	}
	return merged, nil
}

func ApplySourcePath(source, target, path string) (string, error) {
	sourceDoc, err := parseYAMLDocument(source)
	if err != nil {
		return "", fmt.Errorf("parse source YAML: %w", err)
	}
	targetDoc, err := parseYAMLDocument(target)
	if err != nil {
		return "", fmt.Errorf("parse target YAML: %w", err)
	}

	parts := strings.Split(path, ".")
	sourceNode, ok := findPath(documentValue(sourceDoc), parts)
	if !ok {
		deletePath(documentValue(targetDoc), parts)
		return marshalYAMLDocument(targetDoc)
	}
	setNodePathPreservingSourceOrder(documentValue(sourceDoc), documentValue(targetDoc), parts, cloneNode(sourceNode))
	return marshalYAMLDocument(targetDoc)
}

func ApplyListItemChange(source, target, path, itemKey, action string) (string, error) {
	sourceDoc, err := parseYAMLDocument(source)
	if err != nil {
		return "", fmt.Errorf("parse source YAML: %w", err)
	}
	targetDoc, err := parseYAMLDocument(target)
	if err != nil {
		return "", fmt.Errorf("parse target YAML: %w", err)
	}

	parts := strings.Split(path, ".")
	switch action {
	case "add":
		sourceSeq, ok := findSequencePath(documentValue(sourceDoc), parts)
		if !ok {
			return "", fmt.Errorf("source path %q is not a list", path)
		}
		targetSeq := ensureSequencePath(documentValue(targetDoc), parts)
		if err := addListItem(sourceSeq, targetSeq, itemKey); err != nil {
			return "", err
		}
	case "delete":
		targetSeq, ok := findSequencePath(documentValue(targetDoc), parts)
		if !ok {
			return marshalYAMLDocument(targetDoc)
		}
		deleteListItem(targetSeq, itemKey)
	default:
		return "", fmt.Errorf("unsupported list item action %q", action)
	}
	return marshalYAMLDocument(targetDoc)
}

func parseYAML(content string) (any, error) {
	if strings.TrimSpace(content) == "" {
		return map[string]any{}, nil
	}
	var value any
	if err := yaml.Unmarshal([]byte(content), &value); err != nil {
		return nil, err
	}
	return normalize(value), nil
}

func parseYAMLDocument(content string) (*yaml.Node, error) {
	var doc yaml.Node
	if strings.TrimSpace(content) == "" {
		doc.Kind = yaml.DocumentNode
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode}}
		return &doc, nil
	}
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) == 0 {
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode}}
	}
	return &doc, nil
}

func documentValue(doc *yaml.Node) *yaml.Node {
	if doc.Kind != yaml.DocumentNode {
		return doc
	}
	if len(doc.Content) == 0 || doc.Content[0] == nil {
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode}}
	}
	return doc.Content[0]
}

func findSequencePath(root *yaml.Node, path []string) (*yaml.Node, bool) {
	node, ok := findPath(root, path)
	if !ok || node == nil || node.Kind != yaml.SequenceNode {
		return nil, false
	}
	return node, true
}

func ensureSequencePath(root *yaml.Node, path []string) *yaml.Node {
	node, ok := findPath(root, path)
	if ok && node != nil {
		if node.Kind != yaml.SequenceNode {
			node.Kind = yaml.SequenceNode
			node.Tag = "!!seq"
			node.Value = ""
			node.Content = nil
		}
		return node
	}
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	setNodePath(root, path, seq)
	return seq
}

func addListItem(sourceSeq, targetSeq *yaml.Node, itemKey string) error {
	sourceIndex := -1
	var sourceItem *yaml.Node
	for i, child := range sourceSeq.Content {
		if nodeValueKey(child) == itemKey {
			sourceIndex = i
			sourceItem = child
			break
		}
	}
	if sourceItem == nil {
		return fmt.Errorf("list item %q not found in source", itemKey)
	}
	for _, child := range targetSeq.Content {
		if nodeValueKey(child) == itemKey {
			return nil
		}
	}

	insertAt := len(targetSeq.Content)
	for i := sourceIndex - 1; i >= 0; i-- {
		previousKey := nodeValueKey(sourceSeq.Content[i])
		for targetIndex, targetChild := range targetSeq.Content {
			if nodeValueKey(targetChild) == previousKey {
				insertAt = targetIndex + 1
				i = -1
				break
			}
		}
	}
	targetSeq.Content = append(targetSeq.Content, nil)
	copy(targetSeq.Content[insertAt+1:], targetSeq.Content[insertAt:])
	targetSeq.Content[insertAt] = cloneNode(sourceItem)
	return nil
}

func deleteListItem(seq *yaml.Node, itemKey string) bool {
	for i, child := range seq.Content {
		if nodeValueKey(child) != itemKey {
			continue
		}
		seq.Content = append(seq.Content[:i], seq.Content[i+1:]...)
		return true
	}
	return false
}

func nodeValueKey(node *yaml.Node) string {
	if node == nil {
		return ""
	}
	if node.Kind == yaml.ScalarNode {
		return node.Value
	}
	var value any
	if err := node.Decode(&value); err != nil {
		return node.Value
	}
	value = normalize(value)
	if str, ok := value.(string); ok {
		return str
	}
	if key, ok := identityValueKey(value); ok {
		return key
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(data)
}

func identityValueKey(value any) (string, bool) {
	obj, ok := value.(map[string]any)
	if !ok {
		return "", false
	}
	for _, field := range []string{"id", "name", "key", "code"} {
		raw, exists := obj[field]
		if !exists {
			continue
		}
		text := strings.TrimSpace(fmt.Sprint(raw))
		if text == "" {
			continue
		}
		return field + "=" + text, true
	}
	return "", false
}

func normalize(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, val := range typed {
			out[key] = normalize(val)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(typed))
		for key, val := range typed {
			out[fmt.Sprint(key)] = normalize(val)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, val := range typed {
			out[i] = normalize(val)
		}
		return out
	default:
		return typed
	}
}

func flatten(prefix string, value any, out map[string]any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, val := range typed {
			next := key
			if prefix != "" {
				next = prefix + "." + key
			}
			flatten(next, val, out)
		}
	case []any:
		out[prefix] = typed
	default:
		if prefix != "" {
			out[prefix] = typed
		}
	}
}

func uniqueSortedKeys(a, b map[string]any) []string {
	seen := map[string]struct{}{}
	for key := range a {
		seen[key] = struct{}{}
	}
	for key := range b {
		seen[key] = struct{}{}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func firstSensitive(values ...any) rules.SensitiveKind {
	for _, value := range values {
		str, ok := value.(string)
		if !ok {
			continue
		}
		if kind := rules.DetectSensitiveKind(str); kind != rules.SensitiveNone {
			return kind
		}
	}
	return rules.SensitiveNone
}

func findPath(root *yaml.Node, path []string) (*yaml.Node, bool) {
	if len(path) == 0 {
		return root, true
	}
	if root == nil || root.Kind != yaml.MappingNode {
		return nil, false
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != path[0] {
			continue
		}
		if len(path) == 1 {
			return root.Content[i+1], true
		}
		return findPath(root.Content[i+1], path[1:])
	}
	return nil, false
}

func setNodePath(root *yaml.Node, path []string, value *yaml.Node) {
	if len(path) == 0 || root == nil {
		return
	}
	if root.Kind != yaml.MappingNode {
		root.Kind = yaml.MappingNode
		root.Tag = ""
		root.Value = ""
		root.Content = nil
	}
	key := path[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != key {
			continue
		}
		if len(path) == 1 {
			root.Content[i+1] = value
			return
		}
		setNodePath(root.Content[i+1], path[1:], value)
		return
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	if len(path) == 1 {
		root.Content = append(root.Content, keyNode, value)
		return
	}
	next := &yaml.Node{Kind: yaml.MappingNode}
	root.Content = append(root.Content, keyNode, next)
	setNodePath(next, path[1:], value)
}

func setNodePathPreservingSourceOrder(sourceRoot, targetRoot *yaml.Node, path []string, value *yaml.Node) {
	if len(path) == 0 || targetRoot == nil {
		return
	}
	if targetRoot.Kind != yaml.MappingNode {
		targetRoot.Kind = yaml.MappingNode
		targetRoot.Tag = ""
		targetRoot.Value = ""
		targetRoot.Content = nil
	}
	key := path[0]
	for i := 0; i+1 < len(targetRoot.Content); i += 2 {
		if targetRoot.Content[i].Value != key {
			continue
		}
		if len(path) == 1 {
			targetRoot.Content[i+1] = value
			return
		}
		sourceChild, _ := mappingValue(sourceRoot, key)
		setNodePathPreservingSourceOrder(sourceChild, targetRoot.Content[i+1], path[1:], value)
		return
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	if len(path) == 1 {
		insertMappingEntryBySourceOrder(sourceRoot, targetRoot, key, keyNode, value)
		return
	}
	next := &yaml.Node{Kind: yaml.MappingNode}
	insertMappingEntryBySourceOrder(sourceRoot, targetRoot, key, keyNode, next)
	sourceChild, _ := mappingValue(sourceRoot, key)
	setNodePathPreservingSourceOrder(sourceChild, next, path[1:], value)
}

func insertMappingEntryBySourceOrder(sourceRoot, targetRoot *yaml.Node, key string, keyNode, valueNode *yaml.Node) {
	insertAt := len(targetRoot.Content)
	if sourceRoot != nil && sourceRoot.Kind == yaml.MappingNode {
		sourceIndex := mappingKeyIndex(sourceRoot, key)
		for i := sourceIndex - 2; sourceIndex >= 0 && i >= 0; i -= 2 {
			previousKey := sourceRoot.Content[i].Value
			if targetIndex := mappingKeyIndex(targetRoot, previousKey); targetIndex >= 0 {
				insertAt = targetIndex + 2
				break
			}
		}
		if insertAt == len(targetRoot.Content) {
			for i := sourceIndex + 2; sourceIndex >= 0 && i < len(sourceRoot.Content); i += 2 {
				nextKey := sourceRoot.Content[i].Value
				if targetIndex := mappingKeyIndex(targetRoot, nextKey); targetIndex >= 0 {
					insertAt = targetIndex
					break
				}
			}
		}
	}
	targetRoot.Content = append(targetRoot.Content, nil, nil)
	copy(targetRoot.Content[insertAt+2:], targetRoot.Content[insertAt:])
	targetRoot.Content[insertAt] = keyNode
	targetRoot.Content[insertAt+1] = valueNode
}

func mappingValue(root *yaml.Node, key string) (*yaml.Node, bool) {
	if root == nil || root.Kind != yaml.MappingNode {
		return nil, false
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			return root.Content[i+1], true
		}
	}
	return nil, false
}

func mappingKeyIndex(root *yaml.Node, key string) int {
	if root == nil || root.Kind != yaml.MappingNode {
		return -1
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			return i
		}
	}
	return -1
}

func deletePath(root *yaml.Node, path []string) bool {
	if len(path) == 0 || root == nil || root.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != path[0] {
			continue
		}
		if len(path) == 1 {
			root.Content = append(root.Content[:i], root.Content[i+2:]...)
			return true
		}
		return deletePath(root.Content[i+1], path[1:])
	}
	return false
}

func cloneNode(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	out := *node
	if len(node.Content) > 0 {
		out.Content = make([]*yaml.Node, len(node.Content))
		for i, child := range node.Content {
			out.Content[i] = cloneNode(child)
		}
	}
	return &out
}

func marshalYAMLDocument(doc *yaml.Node) (string, error) {
	var b strings.Builder
	encoder := yaml.NewEncoder(&b)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		_ = encoder.Close()
		return "", err
	}
	if err := encoder.Close(); err != nil {
		return "", err
	}
	return b.String(), nil
}
