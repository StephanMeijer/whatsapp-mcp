# Changelog

## 1.0.0 (2026-10-15)


### Features

* add message_context tool ([e839aee](https://github.com/StephanMeijer/whatsapp-mcp/commit/e839aee6b3f713de099bba01ad6f50eba626d12f)), closes [#6](https://github.com/StephanMeijer/whatsapp-mcp/issues/6)
* add read-only mode and recipient allowlist ([334aeb7](https://github.com/StephanMeijer/whatsapp-mcp/commit/334aeb76d1e55a2c1cd3938e44834096b869e542)), closes [#12](https://github.com/StephanMeijer/whatsapp-mcp/issues/12)
* add recent_messages tool for catching up ([4c96021](https://github.com/StephanMeijer/whatsapp-mcp/commit/4c9602117ebfd29c4379b273254d89a4686b5da9)), closes [#5](https://github.com/StephanMeijer/whatsapp-mcp/issues/5)
* apply message edits and deletions to the archive ([40fa24b](https://github.com/StephanMeijer/whatsapp-mcp/commit/40fa24b6915b0ce7f9b8fdcd0f7fbb067a4561a1)), closes [#4](https://github.com/StephanMeijer/whatsapp-mcp/issues/4)
* download media and return images to the client ([865c952](https://github.com/StephanMeijer/whatsapp-mcp/commit/865c952d284c51b89c7004d63d1c9f899b125df8)), closes [#9](https://github.com/StephanMeijer/whatsapp-mcp/issues/9)
* fetch older messages from the phone on demand ([d27890e](https://github.com/StephanMeijer/whatsapp-mcp/commit/d27890eb5ef80df24f255b025bb4c9fcb37c0188)), closes [#8](https://github.com/StephanMeijer/whatsapp-mcp/issues/8)
* keep group chat names up to date ([4d20f61](https://github.com/StephanMeijer/whatsapp-mcp/commit/4d20f61dc1e5199aa794d8bce1399d4145d742b4)), closes [#3](https://github.com/StephanMeijer/whatsapp-mcp/issues/3)
* **mcp:** expose chats, messages, contacts and sending as tools ([343c897](https://github.com/StephanMeijer/whatsapp-mcp/commit/343c89765b7f47c84877ab7807d4f45a66027487))
* merge [@lid](https://github.com/lid) chats and senders into phone-number JIDs ([b5acbca](https://github.com/StephanMeijer/whatsapp-mcp/commit/b5acbcadd1545808fc2edfa45644fb5912559b44)), closes [#2](https://github.com/StephanMeijer/whatsapp-mcp/issues/2)
* send images, video, audio and documents ([6ff4b2e](https://github.com/StephanMeijer/whatsapp-mcp/commit/6ff4b2ec7f46f993b1fe0a71c1d45391342df69f)), closes [#10](https://github.com/StephanMeijer/whatsapp-mcp/issues/10)
* serve the MCP server over streamable HTTP ([dcf8714](https://github.com/StephanMeijer/whatsapp-mcp/commit/dcf8714dc8f8ea7015e2f3461f11426444ec51fe))
* show sender names in message results ([e7b83be](https://github.com/StephanMeijer/whatsapp-mcp/commit/e7b83bea2d0713221e37b8fe56580401c9cf6c31)), closes [#1](https://github.com/StephanMeijer/whatsapp-mcp/issues/1)
* **store:** archive chats and messages in SQLite or Postgres ([2b10a36](https://github.com/StephanMeijer/whatsapp-mcp/commit/2b10a3696a94d1662caeb8be52c72390b2cb1b4d))
* support replies, reactions and marking chats as read ([205757f](https://github.com/StephanMeijer/whatsapp-mcp/commit/205757f140e2b115a610f1f9757b92b0b29a9e90)), closes [#7](https://github.com/StephanMeijer/whatsapp-mcp/issues/7)
* **wa:** link a WhatsApp account and archive its messages ([a5245a9](https://github.com/StephanMeijer/whatsapp-mcp/commit/a5245a98445282707b8cf14e8e8533f889306e1a))


### Bug Fixes

* **deps:** bump golang.org/x/net to v0.60.0 ([f53be88](https://github.com/StephanMeijer/whatsapp-mcp/commit/f53be8848d6dc49e5f28e6e157cdab242b329b35))
