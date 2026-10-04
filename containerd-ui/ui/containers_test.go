package ui

import (
	"reflect"
	"testing"

	"containerd-ui/wsl"
)

func TestTableDataSortsByIDNameAndStatus(t *testing.T) {
	tests := []struct {
		name    string
		field   containerSortField
		rows    []wsl.Container
		wantIDs []string
	}{
		{
			name:  "id",
			field: sortByID,
			rows: []wsl.Container{
				{ID: "z", Name: "Zulu"},
				{ID: "a", Name: "Alpha"},
			},
			wantIDs: []string{"a", "z"},
		},
		{
			name:  "name",
			field: sortByName,
			rows: []wsl.Container{
				{ID: "a", Name: "Zulu"},
				{ID: "z", Name: "Alpha"},
			},
			wantIDs: []string{"z", "a"},
		},
		{
			name:  "status",
			field: sortByStatus,
			rows: []wsl.Container{
				{ID: "running", Status: "running"},
				{ID: "stopped", Status: "exited"},
			},
			wantIDs: []string{"stopped", "running"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data := newDataTable()
			data.setRows(test.rows)
			if ascending := data.toggleSort(test.field); !ascending {
				t.Fatal("first sort should be ascending")
			}
			assertContainerIDs(t, data.getRows(), test.wantIDs)
		})
	}
}

func TestTableDataTogglesSortAndPreservesItOnRefresh(t *testing.T) {
	data := newDataTable()
	data.setRows([]wsl.Container{
		{ID: "c", Name: "Charlie"},
		{ID: "a", Name: "Alpha"},
		{ID: "b", Name: "Bravo"},
	})
	data.toggleSort(sortByName)

	data.setRows([]wsl.Container{
		{ID: "b", Name: "Bravo"},
		{ID: "c", Name: "Charlie"},
		{ID: "a", Name: "Alpha"},
	})
	assertContainerIDs(t, data.getRows(), []string{"a", "b", "c"})

	if ascending := data.toggleSort(sortByName); ascending {
		t.Fatal("second click on the same column should sort descending")
	}
	rows := data.getRows()
	assertContainerIDs(t, rows, []string{"c", "b", "a"})
	for index, row := range rows {
		if got, ok := data.getIndex(row.ID); !ok || got != index {
			t.Fatalf("getIndex(%q) = (%d, %t), want (%d, true)", row.ID, got, ok, index)
		}
	}
}

func TestRunningContainersExcludesStoppedRows(t *testing.T) {
	rows := runningContainers([]wsl.Container{
		{ID: "running", Status: "running"},
		{ID: "up", Status: "Up 5 minutes"},
		{ID: "stopped", Status: "exited"},
	})
	assertContainerIDs(t, rows, []string{"running", "up"})
}

func assertContainerIDs(t *testing.T, rows []wsl.Container, want []string) {
	t.Helper()
	got := make([]string, len(rows))
	for index, row := range rows {
		got[index] = row.ID
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("container IDs = %v, want %v", got, want)
	}
}
