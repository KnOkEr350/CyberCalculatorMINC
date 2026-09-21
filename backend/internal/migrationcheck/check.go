// Package migrationcheck validates the immutable migration registry before
// migrations reach a database. Runtime checksum verification remains the
// final protection for files that were already applied.
package migrationcheck

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type Range struct {
	First int
	Last  int
	Owner string
}

var ReservedRanges = []Range{
	{100, 199, "platform/security"},
	{200, 299, "organizations/directories"},
	{300, 399, "teaching"},
	{400, 499, "internship/practice"},
	{500, 599, "OOP/RPD"},
	{600, 699, "TOP-IT"},
	{700, 799, "schools/Ministry decisions"},
	{800, 899, "reports/workflow/snapshots"},
	{900, 999, "crypto/storage/audit"},
}

var filenamePattern = regexp.MustCompile(`^([0-9]{4})_([a-z0-9]+(?:_[a-z0-9]+)*)\.sql$`)

type manifestEntry struct {
	checksum string
	legacy   bool
}

// Check verifies names, ownership ranges, duplicate versions and SHA-256
// checksums. Registry lines have the format:
// <sha256>  <legacy|active>  <filename>
func Check(dir, manifestPath string) error {
	manifest, err := readManifest(manifestPath)
	if err != nil {
		return err
	}
	directoryEntries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("прочитать каталог миграций: %w", err)
	}

	files := make([]string, 0, len(directoryEntries))
	for _, entry := range directoryEntries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".sql" {
			files = append(files, entry.Name())
		}
	}
	sort.Strings(files)
	if len(files) == 0 {
		return fmt.Errorf("в %s нет SQL-миграций", dir)
	}

	seenFiles := make(map[string]struct{}, len(files))
	versions := make(map[int][]string)
	for _, name := range files {
		match := filenamePattern.FindStringSubmatch(name)
		if match == nil {
			return fmt.Errorf("некорректное имя миграции %q: ожидается NNNN_lower_snake_case.sql", name)
		}
		version, _ := strconv.Atoi(match[1])
		registryEntry, ok := manifest[name]
		if !ok {
			return fmt.Errorf("миграция %s отсутствует в checksum registry", name)
		}
		if registryEntry.legacy && version >= 100 {
			return fmt.Errorf("миграция %s ошибочно помечена legacy", name)
		}
		if !registryEntry.legacy {
			if _, ok := OwnerForVersion(version); !ok {
				return fmt.Errorf("версия %04d файла %s не входит в зарезервированные диапазоны 0100-0999", version, name)
			}
		}
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("прочитать %s: %w", name, err)
		}
		if len(strings.TrimSpace(string(content))) == 0 {
			return fmt.Errorf("миграция %s пуста", name)
		}
		digest := sha256.Sum256(content)
		got := hex.EncodeToString(digest[:])
		if got != registryEntry.checksum {
			return fmt.Errorf("checksum миграции %s изменился: got %s, registry %s", name, got, registryEntry.checksum)
		}
		seenFiles[name] = struct{}{}
		versions[version] = append(versions[version], name)
	}

	for name := range manifest {
		if _, ok := seenFiles[name]; !ok {
			return fmt.Errorf("checksum registry содержит отсутствующую миграцию %s", name)
		}
	}
	for version, names := range versions {
		if len(names) < 2 {
			continue
		}
		allLegacy := true
		for _, name := range names {
			allLegacy = allLegacy && manifest[name].legacy
		}
		if !allLegacy {
			return fmt.Errorf("версия %04d используется несколькими миграциями: %s", version, strings.Join(names, ", "))
		}
	}
	return nil
}

func OwnerForVersion(version int) (string, bool) {
	for _, reserved := range ReservedRanges {
		if version >= reserved.First && version <= reserved.Last {
			return reserved.Owner, true
		}
	}
	return "", false
}

func readManifest(path string) (map[string]manifestEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("открыть checksum registry: %w", err)
	}
	defer file.Close()

	result := make(map[string]manifestEntry)
	scanner := bufio.NewScanner(file)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 || (fields[1] != "legacy" && fields[1] != "active") {
			return nil, fmt.Errorf("%s:%d: ожидается <sha256> <legacy|active> <filename>", path, lineNumber)
		}
		if len(fields[0]) != sha256.Size*2 {
			return nil, fmt.Errorf("%s:%d: некорректный SHA-256", path, lineNumber)
		}
		if _, err := hex.DecodeString(fields[0]); err != nil {
			return nil, fmt.Errorf("%s:%d: некорректный SHA-256: %w", path, lineNumber, err)
		}
		if _, exists := result[fields[2]]; exists {
			return nil, fmt.Errorf("%s:%d: дублируется %s", path, lineNumber, fields[2])
		}
		result[fields[2]] = manifestEntry{checksum: fields[0], legacy: fields[1] == "legacy"}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("прочитать checksum registry: %w", err)
	}
	return result, nil
}
