# Changelog

## [0.6.1](https://github.com/StephanMeijer/whatsapp-mcp/compare/v0.6.0...v0.6.1) (2026-11-11)


### Bug Fixes

* **mcpserver:** raise maxEmojiLen to 64 bytes for complex single emoji ([#164](https://github.com/StephanMeijer/whatsapp-mcp/issues/164)) ([8faf224](https://github.com/StephanMeijer/whatsapp-mcp/commit/8faf224a03be867bb10d9eb5b1fcbcc8bd9fff5e))

## [0.6.0](https://github.com/StephanMeijer/whatsapp-mcp/compare/v0.5.0...v0.6.0) (2026-11-09)


### Features

* add create_group and leave_group behind -enable-group-admin ([d7b22da](https://github.com/StephanMeijer/whatsapp-mcp/commit/d7b22da8c02266e26bd076bcccb9d7532987938a))
* add forward_message to forward archived messages ([7ca7a3b](https://github.com/StephanMeijer/whatsapp-mcp/commit/7ca7a3b807cd4d84ac355176f8ee15738e4604e9))
* add post_status to post status updates ([edd5bb3](https://github.com/StephanMeijer/whatsapp-mcp/commit/edd5bb3c8926f3608ba72edb33f23bc3efba1af6))
* add send_poll to send polls ([881397e](https://github.com/StephanMeijer/whatsapp-mcp/commit/881397e1a41c9688c63978837e537d54ddc001c7))
* add set_group_info and group_invite_link ([05ba56e](https://github.com/StephanMeijer/whatsapp-mcp/commit/05ba56e595a65bcfb1d9a497b984d84593be9f23))
* add update_group_members to add, remove, promote and demote ([84cae4e](https://github.com/StephanMeijer/whatsapp-mcp/commit/84cae4e792c670107bc118c066a3a0ccf67fa4f4))
* archive contact cards and send them with send_contact ([db3bb84](https://github.com/StephanMeijer/whatsapp-mcp/commit/db3bb84afdae9a01f73ef3aff2eb9a70924b0576))
* archive shared locations and send them with send_location ([597a81b](https://github.com/StephanMeijer/whatsapp-mcp/commit/597a81ba8fa8c1c49e984b28331f8f044629b607))
* optionally expire archived messages from disappearing chats ([1e823a0](https://github.com/StephanMeijer/whatsapp-mcp/commit/1e823a018c6cffea8d2a687312c80bc291394b36))
* record forwarded messages and show them in message results ([244e4cf](https://github.com/StephanMeijer/whatsapp-mcp/commit/244e4cfd8f144bc6a2f5591e503b16fc6d485e65))
* separate status updates from chats and add list_status_updates ([a5fa047](https://github.com/StephanMeijer/whatsapp-mcp/commit/a5fa04791787508c80f0dc976d02822a066aaaf2))
* show and set each chat's disappearing-message timer ([dc1f28c](https://github.com/StephanMeijer/whatsapp-mcp/commit/dc1f28c44d093f7e02820f55a109ea44e84961ad))
* track poll votes and add poll_results ([22aace2](https://github.com/StephanMeijer/whatsapp-mcp/commit/22aace2b535304227476c9eaeaef76bbf007e088))


### Bug Fixes

* **build:** wait for Postgres on TCP in test-postgres ([ac19bc6](https://github.com/StephanMeijer/whatsapp-mcp/commit/ac19bc6541b89c669eb068d0db55295bf968c9cf))
* **ci:** check Postgres readiness over TCP in the test job ([a77b453](https://github.com/StephanMeijer/whatsapp-mcp/commit/a77b4539c3cb29053bfbc13070785ee3905613f6))
* **cli:** exit 0 after printing usage for -h ([67f4712](https://github.com/StephanMeijer/whatsapp-mcp/commit/67f47128e6852d2fca272f76beba8b90ea95f026))
* **docker:** mount a tmpfs on /tmp for media downloads ([d3d8255](https://github.com/StephanMeijer/whatsapp-mcp/commit/d3d82559750709d41b6907c00424c2fbe7097b58))
* **mcpserver:** let delete_message take back your own status under post_status's rule ([0046a44](https://github.com/StephanMeijer/whatsapp-mcp/commit/0046a44b5d8f02adeb8a56cedf7703e432b4d127))
* **mcpserver:** log refused post_status calls ([cc6a314](https://github.com/StephanMeijer/whatsapp-mcp/commit/cc6a31401deb31e29bbcd1115b4498107827df8f))
* **mcpserver:** refuse status@broadcast in every write tool but post_status ([9bc5c5a](https://github.com/StephanMeijer/whatsapp-mcp/commit/9bc5c5a770914e263f3a610eb0fde0869e6d351a))
* never expire a message kept in the chat ([db14422](https://github.com/StephanMeijer/whatsapp-mcp/commit/db14422a4248484d4c4c7018e4fb1b9f217fcb57))
* put whatsmeow's IQ errors for group changes in plain words ([fc8cbb7](https://github.com/StephanMeijer/whatsapp-mcp/commit/fc8cbb775338d91c980e8a915802031b63ee5f79))
* report a created group even when whatsmeow errors parsing it ([1d8907b](https://github.com/StephanMeijer/whatsapp-mcp/commit/1d8907be4bf8269aaec94c53ca8f0da9ae9bc50a))
* report unhealthy when WhatsApp is logged out or pairing ended ([64ebb57](https://github.com/StephanMeijer/whatsapp-mcp/commit/64ebb5798841d2b3abed0271d261d88fd31e98c9))
* report update_group_members results as ok, not added ([c406f90](https://github.com/StephanMeijer/whatsapp-mcp/commit/c406f90bb57d350f82f849bc45a6623cca9b83d3))
* **store:** accept a revoked message as the list_messages page cursor ([a14187a](https://github.com/StephanMeijer/whatsapp-mcp/commit/a14187ab532ffc0469ab8ca5703d87a8783583cf))
* **store:** drop receipts and poll votes of deleted and expired messages ([e1ae9ee](https://github.com/StephanMeijer/whatsapp-mcp/commit/e1ae9ee88bba1b9ef8f3295ba29284c83a670b35))
* **store:** drop receipts and votes left by earlier deletions on open ([aa4cbed](https://github.com/StephanMeijer/whatsapp-mcp/commit/aa4cbed2426d1190f22abbc373dfa8eee61ae792))
* **store:** list LIDs seen only as a poll's chat ([faadc1c](https://github.com/StephanMeijer/whatsapp-mcp/commit/faadc1caddb3294993562072b86c88e7e8da947f))
* **store:** match list_chats query in Go so non-ASCII names fold case ([26a51e4](https://github.com/StephanMeijer/whatsapp-mcp/commit/26a51e4baec3166b2142340de8820d8b201ee6bb))
* **store:** merge LIDs seen only in calls or receipts ([cb9bb40](https://github.com/StephanMeijer/whatsapp-mcp/commit/cb9bb40b5648fdf5b70ff7b15babe0cdee560a44))
* **store:** merge LIDs that only voted in polls ([c19bc89](https://github.com/StephanMeijer/whatsapp-mcp/commit/c19bc89d55d5a3bba54ba659c368137af9f93189))
* **store:** move calls when merging a LID into its phone JID ([373908f](https://github.com/StephanMeijer/whatsapp-mcp/commit/373908f1e14b87b30f08978538f55c50e1ab45c4))
* **store:** page list_messages by before_id so same-second messages are kept ([31d0a72](https://github.com/StephanMeijer/whatsapp-mcp/commit/31d0a72c724da757aff561dd4bb70db11f608773))
* **wa:** apply message edits delivered by history sync ([6318b1a](https://github.com/StephanMeijer/whatsapp-mcp/commit/6318b1aaf728c15737d71687160ccf1c2c83e7e4))
* **wa:** bound media downloads by bytes received, not the sender's FileLength ([c6b9a9f](https://github.com/StephanMeijer/whatsapp-mcp/commit/c6b9a9fc45d7af0e6bfb67f7f26bc3e9438280bd))
* **wa:** cancel background work on Stop so shutdown stays bounded ([c40dc27](https://github.com/StephanMeijer/whatsapp-mcp/commit/c40dc27e9a2d80cdd366b4a55adb675b4cd46c60))
* **wa:** don't add group chats from a live timer change ([9ec65bf](https://github.com/StephanMeijer/whatsapp-mcp/commit/9ec65bf8088a21da45f96a4041fb9e3c3cf6d167))
* **wa:** forward a file's own caption, not its archived text ([73ba7ef](https://github.com/StephanMeijer/whatsapp-mcp/commit/73ba7ef3e43fd15e166a7f18b72f0955c3ab0851))
* **wa:** forward an edited message's current caption ([06a1f7d](https://github.com/StephanMeijer/whatsapp-mcp/commit/06a1f7d63a1bf712e1ffbfebee6e914e5dbb5c59))
* **wa:** give messages sent to a disappearing chat its timer ([453b939](https://github.com/StephanMeijer/whatsapp-mcp/commit/453b9391552324229ec51954d7d57aa1fdf54a06))
* **wa:** keep the voice-note flag when forwarding audio ([b3caf05](https://github.com/StephanMeijer/whatsapp-mcp/commit/b3caf058c7320f6aa7af76cd6f16cdb05c35c690))
* **wa:** log and count history-sync messages ParseWebMessage rejects ([fa5068a](https://github.com/StephanMeijer/whatsapp-mcp/commit/fa5068af9b68222e5712fe709f95bb94ec8b52cb))
* **wa:** map a history-sync conversation's [@lid](https://github.com/lid) to its PnJID number ([ba42ade](https://github.com/StephanMeijer/whatsapp-mcp/commit/ba42ade4c678a3b0b1441d0922b0ba5bd29eb6b0))
* **wa:** parse phone-number recipients with NormalizePhone ([9760840](https://github.com/StephanMeijer/whatsapp-mcp/commit/97608407c1727d1081db3ce061844c5e7f85867e))
* **wa:** read vCard phone numbers whose parameters contain a dot ([dd87264](https://github.com/StephanMeijer/whatsapp-mcp/commit/dd872642e171990a81a4f02e5f8601c29a751039))
* **wa:** rely on IsGroup to keep status updates from naming the status chat ([ef8609f](https://github.com/StephanMeijer/whatsapp-mcp/commit/ef8609f0199190dc5e7540cf0272a2eb16159b50))
* **wa:** set the groups flag before the reconcile request flag ([52a0605](https://github.com/StephanMeijer/whatsapp-mcp/commit/52a0605622d9d4107b0c7d234bf3be86b344ba4b))
* **wa:** skip group lookup for status broadcasts ([3f1ca82](https://github.com/StephanMeijer/whatsapp-mcp/commit/3f1ca82b8276bc7a73637a895f48c2c33810086b))
* **wa:** skip history group votes that name no voter ([580d71f](https://github.com/StephanMeijer/whatsapp-mcp/commit/580d71f193bc2123fce808235c3a18d035371ce9))
* **wa:** time out stalled profile-picture downloads ([11a5b6d](https://github.com/StephanMeijer/whatsapp-mcp/commit/11a5b6d34db8c0ab319b66edc10517c4f80af2e3))


### Performance

* **store:** index messages.sender_jid for LID merges ([cc74097](https://github.com/StephanMeijer/whatsapp-mcp/commit/cc7409763e32766315ab3584d3307caebec9e5ae))
* **wa:** coalesce reconcile passes into one worker that Stop waits for ([c2b075d](https://github.com/StephanMeijer/whatsapp-mcp/commit/c2b075d5a657b94d5338e4144a12a2617c070800))

## [0.5.0](https://github.com/StephanMeijer/whatsapp-mcp/compare/v0.4.0...v0.5.0) (2026-11-05)


### Features

* add a Docker Compose setup with Postgres ([18930fb](https://github.com/StephanMeijer/whatsapp-mcp/commit/18930fb1dde48ded2f6f2c549beb89300d140e1c))


### Bug Fixes

* make the postgres health check use TCP ([cbfa529](https://github.com/StephanMeijer/whatsapp-mcp/commit/cbfa529deadc6127b25f4a9b00db12d6d88307f4))

## [0.4.0](https://github.com/StephanMeijer/whatsapp-mcp/compare/v0.3.0...v0.4.0) (2026-11-03)


### ⚠ BREAKING CHANGES

* the server no longer starts with a WHATSAPP_MCP_TOKEN shorter than 32 characters. Replace a short token with one from `openssl rand -hex 32` and update it in your MCP client.

### Features

* archive polls ([c1bd2a6](https://github.com/StephanMeijer/whatsapp-mcp/commit/c1bd2a60bc647070a8caf9ccc4a7fa425650fc3f))
* archive, pin and mute chats ([d4f357a](https://github.com/StephanMeijer/whatsapp-mcp/commit/d4f357ab6fdf07c9460124f8997192a56e708a02))
* archive, pin and mute chats ([a428cb6](https://github.com/StephanMeijer/whatsapp-mcp/commit/a428cb6584b3996d7fdaca6f57e6ed082d92aa25))
* track delivered, read and played receipts for sent messages ([75ef68d](https://github.com/StephanMeijer/whatsapp-mcp/commit/75ef68d3f47dc6426fcfad44026f046c016a680a))
* track delivered, read and played receipts for sent messages ([ad622b2](https://github.com/StephanMeijer/whatsapp-mcp/commit/ad622b2663cc6d0c1420685da06e4a7482f4f9d1))


### Bug Fixes

* keep muted-forever chats muted when merging [@lid](https://github.com/lid) JIDs ([96fbea1](https://github.com/StephanMeijer/whatsapp-mcp/commit/96fbea10f95d285ed7c0961ecc936703ebe6188b))
* move receipts when merging [@lid](https://github.com/lid) JIDs into phone numbers ([e517ee7](https://github.com/StephanMeijer/whatsapp-mcp/commit/e517ee747dfb1e6b511ad73186a8c381410886a0))
* refuse auth tokens shorter than 32 characters ([80d4d1f](https://github.com/StephanMeijer/whatsapp-mcp/commit/80d4d1fd78f9b982ed6c9aefe1dfa3474a2e6b49))

## [0.3.0](https://github.com/StephanMeijer/whatsapp-mcp/compare/v0.2.0...v0.3.0) (2026-10-22)


### Features

* add a systemd user unit and example env file ([26b5fdf](https://github.com/StephanMeijer/whatsapp-mcp/commit/26b5fdf1c4972a11d47461acca84560dd3fe1d58))
* add contact_info and get_profile_picture tools ([789acc9](https://github.com/StephanMeijer/whatsapp-mcp/commit/789acc9abc1528a293e130e17cf5eb07f9e4382d))
* add contact_info and get_profile_picture tools ([3b4d507](https://github.com/StephanMeijer/whatsapp-mcp/commit/3b4d50707044ad2cdd3ed8aed246ed3f8ca62073))
* add delete_message tool ([9ff6be0](https://github.com/StephanMeijer/whatsapp-mcp/commit/9ff6be039d29744e850f98ea340b29e974a4c6e0)), closes [#37](https://github.com/StephanMeijer/whatsapp-mcp/issues/37)
* add edit_message tool ([6387a47](https://github.com/StephanMeijer/whatsapp-mcp/commit/6387a478b8f4d91b11899161f0d446a2da8d96f9)), closes [#23](https://github.com/StephanMeijer/whatsapp-mcp/issues/23)
* add group_info tool ([6c16198](https://github.com/StephanMeijer/whatsapp-mcp/commit/6c16198938990f22d0d22e923a7f679f05c4857f))
* add group_info tool ([f9a426d](https://github.com/StephanMeijer/whatsapp-mcp/commit/f9a426de7d505cbed1dc1ed127f5b6b48a089658))


### Bug Fixes

* keep reply refs only for own messages and drop them on edit ([98dc7b3](https://github.com/StephanMeijer/whatsapp-mcp/commit/98dc7b3914533d0b9c56f6de87b4d0750ad557f0))
* keep the quote when editing a reply ([a014140](https://github.com/StephanMeijer/whatsapp-mcp/commit/a014140777a760f4b9dd3de4e269242cff10f81d))

## [0.2.0](https://github.com/StephanMeijer/whatsapp-mcp/compare/v0.1.0...v0.2.0) (2026-10-15)


### Features

* add check_number tool ([1a18aa8](https://github.com/StephanMeijer/whatsapp-mcp/commit/1a18aa8226189c744e38e9aeefd9930d8a419f83)), closes [#29](https://github.com/StephanMeijer/whatsapp-mcp/issues/29)
* show incoming calls and add a reject_call tool ([3b56173](https://github.com/StephanMeijer/whatsapp-mcp/commit/3b56173519689c83ccc07ffb4e718c9bce934007)), closes [#21](https://github.com/StephanMeijer/whatsapp-mcp/issues/21)


### Bug Fixes

* **mcpserver:** apply the recipient allowlist to check_number ([0cab4cf](https://github.com/StephanMeijer/whatsapp-mcp/commit/0cab4cfa411cfbbadd4e9c4bfa37052824251709))
* **wa:** do not report LID-only answers as not on WhatsApp ([723fee9](https://github.com/StephanMeijer/whatsapp-mcp/commit/723fee9b2d0f4fda3284fcc41faa3650adcd8ebb))

## 0.1.0 (2026-10-15)


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
