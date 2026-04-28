package masker

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// placeholderPattern 定义占位符的正则表达式格式：${TYPE_N}
var placeholderPattern = regexp.MustCompile(`^\$\{[A-Z_]+[0-9]+\}$`)

// ValidatePlaceholder 校验占位符格式是否符合 ${TYPE_N} 规范。
func ValidatePlaceholder(placeholder string) error {
	if !placeholderPattern.MatchString(placeholder) {
		return fmt.Errorf("占位符格式无效: %s", placeholder)
	}

	return nil
}

// ApplyOriginalToPlaceholder 应用原始值到占位符的替换，按长字符串优先排序。
func ApplyOriginalToPlaceholder(input string, originalToPlaceholder map[string]string) string {
	return applySortedReplacements(input, originalToPlaceholder)
}

// ApplyPlaceholderToOriginal 应用占位符到原始值的替换，按长字符串优先排序。
func ApplyPlaceholderToOriginal(input string, placeholderToOriginal map[string]string) string {
	return applySortedReplacements(input, placeholderToOriginal)
}

// MergeMappings 合并可信 LLM 返回的映射条目，并校验是否存在冲突。
func MergeMappings(originalToPlaceholder map[string]string, placeholderToOriginal map[string]string, entries []MappingEntry) error {
	for _, entry := range entries {
		if entry.Original == "" {
			return fmt.Errorf("映射条目的原始值为空")
		}

		if err := ValidatePlaceholder(entry.Placeholder); err != nil {
			return err
		}

		// 检查原始值是否已有不同的占位符映射
		if existingPlaceholder, ok := originalToPlaceholder[entry.Original]; ok && existingPlaceholder != entry.Placeholder {
			return fmt.Errorf("原始值 %s 存在冲突的占位符映射", entry.Original)
		}

		// 检查占位符是否已有不同的原始值映射
		if existingOriginal, ok := placeholderToOriginal[entry.Placeholder]; ok && existingOriginal != entry.Original {
			return fmt.Errorf("占位符 %s 存在冲突的原始值映射", entry.Placeholder)
		}

		originalToPlaceholder[entry.Original] = entry.Placeholder
		placeholderToOriginal[entry.Placeholder] = entry.Original
	}

	return nil
}

// applySortedReplacements 应用替换映射，按字符串长度降序排序以优先替换长字符串。
func applySortedReplacements(input string, replacements map[string]string) string {
	keys := make([]string, 0, len(replacements))
	for key := range replacements {
		keys = append(keys, key)
	}

	// 按长度降序排序，相同长度按字典序排序
	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) == len(keys[j]) {
			return keys[i] < keys[j]
		}
		return len(keys[i]) > len(keys[j])
	})

	output := input
	for _, key := range keys {
		output = strings.ReplaceAll(output, key, replacements[key])
	}

	return output
}