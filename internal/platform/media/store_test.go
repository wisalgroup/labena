package media

import "testing"

func TestBaselineContainsOnlyAssetTable(t *testing.T) {
	db, _ := databaseFixture(t)
	var count int
	if err := db.NewRaw("SELECT count(*) FROM pg_tables WHERE schemaname=current_schema() AND tablename IN ('media','media_libraries')").Scan(t.Context(), &count); err != nil || count != 0 {
		t.Fatal("legacy tables remain", count, err)
	}
}
