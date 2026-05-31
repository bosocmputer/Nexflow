package catalog

import "testing"

func TestParseProductV4ResponseNexflowNativeMeta(t *testing.T) {
	body := []byte(`{
		"success": true,
		"data": [
			{
				"code": "BF00002",
				"name_1": "สินค้า",
				"name_2": "",
				"unit_standard": "ชิ้น",
				"group_code": "G1",
				"balance_qty": 3,
				"price": 12.5,
				"image_count": 2,
				"primary_image_roworder": 1,
				"primary_image_guid": "c4331ae7-7b7f-429d-85b4-4ad2e26607f9",
				"primary_image_bytes": 1045269
			}
		],
		"meta": {"total": 201, "page": 2, "size": 100}
	}`)

	items, page, maxPage, err := parseProductV4Response(body)
	if err != nil {
		t.Fatal(err)
	}
	if page != 2 || maxPage != 3 {
		t.Fatalf("page=%d maxPage=%d, want 2/3", page, maxPage)
	}
	if len(items) != 1 {
		t.Fatalf("items=%d", len(items))
	}
	if items[0].GroupCodeV1 != "G1" || items[0].Price != 12.5 {
		t.Fatalf("item = %+v", items[0])
	}
	if items[0].ImageCount != 2 || items[0].PrimaryImageRoworder == nil || *items[0].PrimaryImageRoworder != 1 {
		t.Fatalf("image metadata = %+v", items[0])
	}
	if items[0].PrimaryImageGuid == "" || items[0].PrimaryImageBytes != 1045269 {
		t.Fatalf("image metadata = %+v", items[0])
	}
}
