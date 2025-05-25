# rspamd-iscan

rspamd-iscan is a daemon that monitors IMAP mailboxes and forwards new mails to
[rspamd](https://rspamd.com) for spam analysis or ham classification.
It is similar to [isbg](https://gitlab.com/isbg/isbg) but uses rspamd instead of
Spamassassin.


rspamd-iscan scans new mails arriving in a _ScanMailbox_, if they are classified
as Spam they are moved to the _SpamMailbox_, otherwise to the _InboxMailbox_.
The _ScanMailBox_ is monitored via IMAP IDLE for new E-Mails and additionally
also scanned periodically. \
Mails in _HamMailbox_ and _UndetectedMailbox_ are processed periodically and fed
to rspamd to be learned as Ham/Spam.
Mails that have been learned as Ham are moved to _InboxMailbox_, learned Spam
mails are moved to _SpamMailbox_.

The UID of the last scanned mail from _InboxMailbox_ is stored in a state file.
This allows to use the same mailbox as _InboxMailbox_ and _ScanMailBox_.

## Configuration

rspamd-iscan can be configured via a TOML configuration file.
Example:

```toml
RspamdURL           = "http://192.168.178.2:11334"
RspamdPassword      = "iwonttellyou"
ImapAddr            = "my-imap-server:993"
ImapUser            = "rickdeckard"
ImapPassword        = "zhora"
InboxMailbox        = "INBOX"
SpamMailbox         = "Spam"
HamMailbox          = "Ham"
UndetectedMailbox   = "Undetected"
ScanMailbox         = "Unscanned"
SpamThreshold       = 10.0
```

The location of the configuration and state file can be specified via command
line parameters.
