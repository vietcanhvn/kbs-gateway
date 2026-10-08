# RunningHub workflows

RunningHub (runninghub.ai) runs ComfyUI workflows in the cloud. The gateway keeps
a **workflow registry**: each entry turns one RunningHub workflow into a normal
gateway model (for example `rh-h3-real-skin`) that KSB calls like any other model
through the DC-Media API. The registry is executor-independent; `runninghub` is
the only executor today, `comfyui` (our own ComfyUI / RunPod servers) is reserved.

## Requests KSB can send

| Endpoint | Used for |
|---|---|
| `POST /v1/video/generations` | Video (and audio) workflows. Asynchronous task: poll `GET /v1/video/generations/{id}`; the result is served by the gateway at `/v1/videos/{id}/content` (and a signed public link in `result_url`). |
| `POST /v1/images/generations`, `/v1/images/edits` | Image workflows. The gateway waits for RunningHub (up to 10 minutes) and returns `b64_json` (default) or `url` when `response_format` is `url`. |

Request fields are mapped by the workflow's input mapping:

| DC-Media field | Role |
|---|---|
| `prompt` | `prompt` (after the prompt template) |
| `metadata.negative_prompt` | `negative_prompt` |
| `image`, `images[]`, `metadata.reference_images[]` (in this order) | `image` #1, #2, ... — KSB's `@image1` is the first one |
| `metadata.reference_videos[]` / `metadata.reference_audios[]` | `video` / `audio` |
| `metadata.last_frame_image` | `last_frame` |
| `duration` | `duration` (clamped to the mapping's min/max) |
| `metadata.ratio`, or `width`/`height` | `aspect_ratio` (through the ratio table; the closest ratio wins) |
| `width`, `height` | `width`, `height` |
| `metadata.seed` | `seed` (random when not sent) |

Sending more images/videos/audios than the workflow has inputs is rejected with
HTTP 400 before anything is uploaded.

## Adding a workflow (admin)

1. **Channel** (once): Channels → Add → type **RunningHub**, paste the RunningHub
   API key, keep the base URL `https://www.runninghub.ai` (use
   `https://www.runninghub.cn` for a China-site key). Leave the model list empty.
2. **Prepare the workflow on RunningHub**: open it in *your* account (clone a
   community workflow first), make sure every input you want is enabled (not
   bypassed), **run it once successfully and save** — the API refuses workflows
   that were never run (error 810).
3. Models → **RunningHub workflows** → **Add workflow**:
   - paste the `/workflow/<id>` link and press **Fetch** (downloads the API JSON
     with the channel key), or upload the exported **API JSON** — optionally also
     the full editor JSON, which shows bypassed nodes and custom-node packages;
   - read the analysis: **model files** (must exist in the RunningHub account),
     custom node packages, inputs that exist but are disabled;
   - check the suggested inputs (prompt, images, duration with min/max, ratio
     table, seed) and the output node;
   - optional prompt template: prefix/suffix, a template with `{{prompt}}`, and
     reference-tag rewriting such as `@image1` → `<Picture {n}>`;
   - model name, title, output type, billing (*per request* or *× seconds*),
     GPU (*Plus* = 48 GB) → **Save**.
4. **Test run** in the same panel (runs one real task on RunningHub; RunningHub
   charges the account, the gateway does not bill it).
5. Set the model's price under System settings → Billing → **Model Pricing**.
   With billing *× seconds* the price is per second.
6. Switch the workflow **on**. This adds the model name to every RunningHub channel.

## Models and LoRAs

- A workflow can only use model files that the RunningHub account owning the API
  key can load. Files from RunningHub's public model library work everywhere.
- Cloning a community workflow works when all its models are in the public
  library or the author shared them. Models that are private to the author fail
  at run time ("Value not in list" / missing file); the gateway reports this as
  *a model or input file is missing in the RunningHub account*. Upload those files
  to your own account (or pick a public replacement) and run the workflow once.
- RunningHub's LoRA upload API (`POST /api/openapi/getLoraUploadUrl`, fields
  `apiKey`, `loraName`, `md5Hex`; then PUT the file to the returned URL) only
  works with the **RHLoraLoader** node, not with the standard `LoraLoader`.

## Limits and behaviour

- Uploads: 30 MB per file (RunningHub limit); larger files are rejected.
- Concurrency depends on the RunningHub plan; a full queue (421) or busy GPUs
  (415) are returned as HTTP 429 so the gateway can retry another channel.
- Not enough balance (416), invalid key, or a workflow error at submit time are
  shown with an explanation; failed tasks are refunded like other task channels.
- RunningHub does not document how long output URLs stay valid. The gateway only
  exposes its own content URL and streams from RunningHub when it is requested,
  so KSB should download results soon after completion.
- Cancel (`DELETE /v1/videos/{id}`) calls RunningHub's cancel and reports success
  only after RunningHub confirms the task stopped.

## RunningHub API used

Reference: <https://www.runninghub.ai/runninghub-api-doc-en/> (markdown mirror:
<https://s.apifox.cn/apidoc/docs-site/5441421/>). All calls are `POST` with the
key in the JSON body (`apiKey`) plus `Authorization: Bearer <key>`.

| Call | Path |
|---|---|
| Upload input | `/task/openapi/upload` (multipart `file`, `apiKey`, `fileType=input`) → `data.fileName` |
| Create task | `/task/openapi/create` (`workflowId`, `nodeInfoList[{nodeId, fieldName, fieldValue}]`, `instanceType`) |
| Status | `/task/openapi/status` → `QUEUED` / `RUNNING` / `SUCCESS` / `FAILED` |
| Outputs | `/task/openapi/outputs` → `[{fileUrl, fileType, nodeId, ...}]`; code 804 running, 813 queued, 805 failed with `failedReason` |
| Cancel | `/task/openapi/cancel` |
| Workflow JSON | `/api/openapi/getJsonApiFormat` → `data.prompt` (JSON string) |
| Account | `/uc/openapi/accountStatus` (body field `apikey`) |
