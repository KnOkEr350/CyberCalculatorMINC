package filestore

import (
	"os"
	"sync"
	"testing"
)

// QA-07: одновременная загрузка одного имени не должна давать двум запросам
// один и тот же файл — иначе они затрут работу друг друга.
func TestConcurrentCreateGivesOnlyOneWinner(t *testing.T) {
	root := t.TempDir()
	const attempts = 16
	var wg sync.WaitGroup
	var mu sync.Mutex
	created := []string{}
	failures := 0
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f, path, err := Create(root, "entry", "same-name")
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failures++
				return
			}
			f.Close()
			created = append(created, path)
		}()
	}
	wg.Wait()
	if len(created) != 1 {
		t.Fatalf("файл создан %d раз, одновременная загрузка должна выиграть ровно один раз", len(created))
	}
	if failures != attempts-1 {
		t.Fatalf("отказов %d, ожидалось %d", failures, attempts-1)
	}
}

// Оборванная загрузка не оставляет файл, который выглядит целым: незавершённый
// файл удаляется вызывающим, и хранилище возвращается в исходное состояние.
func TestInterruptedUploadLeavesNoTrace(t *testing.T) {
	root := t.TempDir()
	f, path, err := Create(root, "entry", "partial")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("половина файла")); err != nil {
		t.Fatal(err)
	}
	f.Close()

	// Обработчик обрывает загрузку и убирает за собой.
	if err := Remove(root, path); err != nil {
		t.Fatalf("незавершённый файл должен удаляться: %v", err)
	}
	// Create возвращает полный путь, поэтому проверяем его напрямую.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("после обрыва файл остался в хранилище: %v", err)
	}
	// Имя снова свободно: повторная загрузка не упирается в мусор.
	again, _, err := Create(root, "entry", "partial")
	if err != nil {
		t.Fatalf("после обрыва имя должно освобождаться: %v", err)
	}
	again.Close()
}

// Повторное удаление уже удалённого файла не ломает хранилище и не трогает
// соседние записи.
func TestRemoveIsSafeToRepeat(t *testing.T) {
	root := t.TempDir()
	first, firstPath, err := Create(root, "entry", "one")
	if err != nil {
		t.Fatal(err)
	}
	first.Close()
	second, secondPath, err := Create(root, "entry", "two")
	if err != nil {
		t.Fatal(err)
	}
	second.Close()

	if err := Remove(root, firstPath); err != nil {
		t.Fatal(err)
	}
	if err := Remove(root, firstPath); err == nil {
		t.Fatal("повторное удаление несуществующего файла должно возвращать ошибку, а не молчать")
	}
	if _, err := os.Stat(secondPath); err != nil {
		t.Fatalf("соседний файл пострадал: %v", err)
	}
}
