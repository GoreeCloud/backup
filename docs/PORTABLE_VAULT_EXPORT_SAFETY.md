# Portable vault exports — privacy and recovery safety gates

**Status:** Operator guidance for sensitive backup handling; not evidence of a completed migration, tested restore, secure storage, or approved disposal. This is product-specific GoreeCloud Backup documentation, subordinate to the authoritative GoreeCloud Drive policies.

## Threat model

Bitwarden-compatible and Vaultwarden vault exports may contain account passwords, recovery information, tokens, notes, identities and other sensitive data. An export marked `"encrypted": false` is **not a safe encrypted portable backup**. A local directory's file permissions, NTFS/FAT mount options, a hidden filename, and USB physical possession do not provide encryption at rest. Assume a lost or removed unencrypted disk can be read on another computer. The presence of an export does not prove it was publicly exposed or exfiltrated.

**Do not put vault exports, encryption passwords, generated secret keys, or recovered content into Git, issues, CI logs, task descriptions, chat, email, or unencrypted general-purpose storage.** Do not publish paths identifying private data locations. Avoid copying plaintext exports to a staging filesystem merely to inspect them.

## Recommended protected export methods

- **Portable encrypted JSON:** Use the supported Bitwarden/Vaultwarden vault export interface and select *encrypted JSON, password protected* when that option is available. Use a unique, high-entropy encryption password with a verified, separate recovery record. The exact UI and availability depend on the installed server/client version; confirm support on the actual authorized account.
- **Account-restricted encrypted JSON:** This format can only be imported into the originating account/organization. A change to its encryption key can invalidate the export. It is not equivalent to a portable recovery copy for another account or installation.
- **Encrypted Kopia snapshots:** An authorized, trusted Kopia repository encrypts snapshot contents and filenames using an operator-controlled repository password. This can protect copies of original export files, but it cannot retrospectively encrypt pre-existing plaintext files on a portable disk. GoreeCloud Backup's development API must not be assumed to be production-ready or deployed.
- **Whole-volume encryption:** An appropriately configured encrypted removable volume can protect all files at rest when locked. Merely changing NTFS permissions or renaming files does not achieve this. Do not reformat, repartition, encrypt an in-use device, or change its mount options without a full, tested recovery and an approved change plan.

**Recovery credentials must remain available independently of the source device and backup destination.** Do not silently invent or escrow a password in application logs or a project repository. A backup that cannot be decrypted by an authorized future operator is not a recoverable backup.

## Acceptance workflow (no destructive actions until all gates pass)

1. **Inventory with minimum disclosure.** Check authorized file ownership, sizes, timestamps, storage medium and export *metadata* without printing vault records. Note which vault/organization, if any, is included and whether file attachments or other unsupported item types require independent backup. Do not copy or paste item contents into reports.
2. **Prepare a trusted target.** Confirm that the destination is encrypted, separate from the plaintext original, capacity-adequate, under least-privilege access, and recoverable if the original device is lost. Confirm custody of the recovery password/key and an offline recovery procedure.
3. **Generate or migrate the protected backup.** Prefer creating a fresh password-protected encrypted JSON export in the actual supported vault client. When using Kopia for an existing plaintext export, run only on an authorized, trusted host with an encrypted repository, and verify that Kopia's repository password is safeguarded. Never use an unreviewed script or an external conversion service to process plaintext secrets.
4. **Verify before retirement.** Confirm the generated artifact is structurally correct and marked encrypted, then test **decryption/import in an isolated authorized recovery environment** or perform an approved, complete Kopia restoration to a new private destination. A JSON flag, checksum, successful copy, or green snapshot job alone does **not** prove restore viability. Verify representative expected data privately and ensure only authorized operators can access it.
5. **Preserve completeness.** Determine whether organization-owned records, attachments, Sends, trash or other data are excluded by the selected export format. Arrange additional protected, separately verified recovery for needed exclusions.
6. **Retire old plaintext copies only after acceptance.** Obtain specific approval for removal, verify the new protected copies and separate key recovery, identify every plaintext duplicate and temporary file, and follow the media-specific sanitization procedure. Ordinary deletion, `chmod`, or `shred` on flash/NTFS **cannot be assumed to securely erase all remnants**. If physical-media sanitization is not feasible, manage the device as sensitive until it is securely retired or destroyed. Do not interrupt unrelated backups on a shared device.
7. **Response if exposure is suspected.** Preserve relevant evidence, assess actual access and sharing, revoke/rotate affected credentials according to the incident plan, and follow Wardveil response controls and GoreeShield minimization. The mere existence of a plaintext export does not establish credential compromise.

## Release / remediation stop conditions

- [ ] The destination's at-rest encryption and authorization have been verified, not assumed from permissions.
- [ ] Encryption password/key custody and independent disaster recovery have been tested.
- [ ] A protected copy exists and was successfully restored/imported using a representative isolated test.
- [ ] Necessary organization data, attachments and other excluded items have independent recovery.
- [ ] Owner approval exists for retiring unencrypted source copies, with an appropriate media sanitization plan.
- [ ] Relevant security task and private evidence have been updated without publishing secrets.

**If any gate is missing:** retain existing files without making additional plaintext copies, restrict physical custody and local access, and keep the security obligation open. Do not claim the risk is resolved.

## Primary references

- [Bitwarden — Encrypted Exports](https://bitwarden.com/help/encrypted-export/)
- [Bitwarden — Export Vault Data](https://bitwarden.com/help/export-your-data/)
- [Kopia — Encryption](https://kopia.io/docs/advanced/encryption/)
- [Kopia — Frequently Asked Questions](https://kopia.io/docs/faqs/)
