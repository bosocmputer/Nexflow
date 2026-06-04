# OpenAI Batch Embeddings Example

ตัวอย่างนี้เป็น canary flow สำหรับย้าย `POST /api/catalog/embed-all` จากการยิง OpenRouter ทีละรายการ ไปเป็น OpenAI Batch API แบบ async โดยยังคง embedding 1536 มิติให้เข้ากับ `sml_catalog.embedding` เดิม

## When To Use

- ใช้กับ bulk catalog embedding เช่น sync สินค้า 3,006 รายการแล้วต้อง embed ทั้งก้อน
- ไม่ใช้กับ embed สินค้าเดี่ยวหลังสร้างสินค้าใหม่ เพราะงานเดี่ยวควรตอบเร็วและใช้ synchronous provider เดิมได้
- Batch output order ไม่การันตีว่าตรง input order จึงต้องใช้ `custom_id` map กลับ `item_code`

## JSONL Input

แต่ละบรรทัดคือหนึ่ง request ไป `/v1/embeddings`

```jsonl
{"custom_id":"catalog:BF00002","method":"POST","url":"/v1/embeddings","body":{"model":"text-embedding-3-small","input":"BF00002\nPUMPKIN - รุ่น 17811 PTT-SI40P หัวแร้งบัดกรีแบบปากกา 40 W\nunit:ชิ้น","dimensions":1536,"encoding_format":"float"}}
{"custom_id":"catalog:BF0004","method":"POST","url":"/v1/embeddings","body":{"model":"text-embedding-3-small","input":"BF0004\nเครื่องนวดหลังไฟฟ้า HODEKT บรรเทาอาการปวดเมื่อยกล้ามเนื้อ หัวนวด 20 หัว แม่เหล็ก\nunit:ชิ้น","dimensions":1536,"encoding_format":"float"}}
{"custom_id":"catalog:BFAPIQAPRD260518001","method":"POST","url":"/v1/embeddings","body":{"model":"text-embedding-3-small","input":"BFAPIQAPRD260518001\nBF API QA Product 2026-05-18\nunit:ชิ้น","dimensions":1536,"encoding_format":"float"}}
```

## Submit Flow

```bash
curl https://api.openai.com/v1/files \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -F purpose=batch \
  -F file=@catalog-embeddings.jsonl
```

นำ `id` จาก file response ไปสร้าง batch:

```bash
curl https://api.openai.com/v1/batches \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "input_file_id": "file_xxx",
    "endpoint": "/v1/embeddings",
    "completion_window": "24h",
    "metadata": {
      "feature": "nexflow_catalog_embed",
      "tenant": "SML1_2026"
    }
  }'
```

Poll:

```bash
curl https://api.openai.com/v1/batches/batch_xxx \
  -H "Authorization: Bearer $OPENAI_API_KEY"
```

Download output when `status=completed` and `output_file_id` is present:

```bash
curl https://api.openai.com/v1/files/file_output_xxx/content \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -o catalog-embeddings-output.jsonl
```

## Output Mapping

Output JSONL shape is one response per line. Use `custom_id` to derive `item_code`.

```json
{
  "custom_id": "catalog:BF00002",
  "response": {
    "status_code": 200,
    "body": {
      "data": [
        {
          "embedding": [0.001, -0.002]
        }
      ],
      "usage": {
        "prompt_tokens": 32,
        "total_tokens": 32
      }
    }
  },
  "error": null
}
```

Implementation mapping:

- `custom_id="catalog:<item_code>"` -> update `sml_catalog.item_code`
- `response.status_code=200` and `data[0].embedding` exists -> `SetEmbedding(item_code, embedding, "text-embedding-3-small")`
- `error != null` or missing embedding -> `SetEmbeddingError(item_code)`
- record `batch_id`, `input_file_id`, `output_file_id`, `request_counts`, and stable status in a new job table

## Nexflow Integration Sketch

1. Add `OPENAI_API_KEY` and provider config, keeping current OpenRouter embed path as fallback.
2. Add `catalog_embedding_jobs` table:
   - `id`, `provider`, `batch_id`, `input_file_id`, `output_file_id`, `status`
   - `total`, `completed`, `failed`, `created_at`, `completed_at`, `last_error`
3. Change `POST /api/catalog/embed-all`:
   - if provider is `openai_batch`, create JSONL from pending rows, upload file, create batch, return `202` with `job_id`
   - if provider is `openrouter_sync`, keep current behavior
4. Add polling endpoint/job:
   - `GET /api/catalog/embed-jobs/:id`
   - background poll OpenAI batch status, download output once completed, update `sml_catalog`
5. UI `/settings/catalog`:
   - show provider, job status, completed/failed/total, and last checked time

## Official References

- OpenAI Batch guide: https://platform.openai.com/docs/guides/batch
- Create batch API reference: https://platform.openai.com/docs/api-reference/batch/create
- Create embeddings API reference: https://platform.openai.com/docs/api-reference/embeddings/create
