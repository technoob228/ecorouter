# EcoRouter Roadmap

## Phase 3: Developer Experience

### Claude Code Compatibility
- Full compatibility as a drop-in replacement for Claude Code CLI
- Support Claude Code's expected API format and endpoints
- Allow users to point Claude Code at EcoRouter instead of Anthropic directly

### Usage History & Spending Dashboard
- Per-request cost tracking visible in chat UI
- Show token count and context size for each request
- Historical usage breakdown by model, day, week, month
- Cost comparison: "you spent $X, saved $Y vs direct API"

### Multi-Key Management
- Create multiple API keys per account with individual limits
- Key labels/names for different projects or environments
- Per-key usage tracking and spending caps

## Phase 4: Smart Routing & Eco Optimization

### AI-Optimized Model Selection ("Eco Mode")
- Automatically route requests to the most efficient model that can handle the task
- Analyze prompt complexity and pick the cheapest/greenest model that delivers quality
- Skip flagship models for simple tasks → less compute → less energy → lower cost
- User toggle: "Let EcoRouter choose" vs manual model selection

### Eco Scores & Model Evaluation
- Rate models and AI companies on environmental responsibility
- Metrics: compute efficiency (quality per FLOP), data center energy sources, carbon commitments
- Show eco score badges on model selector
- Publish transparent methodology

## Phase 5: Social & Transfer Features

### Credit Transfers
- Send credits to another user by email
- Gift links: generate a URL that grants $X credit to whoever claims it
- Team/org accounts with shared balance

### Artifacts & Storage
- Rich output rendering (code, tables, diagrams) like Claude Artifacts
- Save and share artifacts across chats
- Project workspaces: group chats + files + instructions together

### System Instructions
- Per-chat or per-project system prompts
- Reusable instruction templates

## Phase 6: Full Model Catalog & Multimodal

### All OpenRouter Models
- Expose the full OpenRouter model catalog via API and UI
- Dynamic model list that updates automatically
- Filter by capability: vision, audio, function calling, etc.

### Multimodal Support
- Image generation models
- Audio input/output (speech-to-text, text-to-speech)
- Video understanding

### Connectors & Integrations
- Connect external tools: web browsing, code execution, file storage
- Plugin system for extending model capabilities
- Webhook/callback support for async workflows
