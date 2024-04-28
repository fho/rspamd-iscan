# rspamd-iscan

rspamd-iscan is a daemon that monitors IMAP mailboxes and forwards new mails to 
[rspamd](https://rspamd.com) for spam analysis or ham classification.
It is similar to [isbg](https://gitlab.com/isbg/isbg) but uses rspamd instead of
Spamassassin.


rspamd-iscan scans new mails arriving in a _ScanMailbox_, if they are classified
as Spam they are moved to the _SpamMailbox_, otherwise to the _InboxMailbox_.
The _ScanMailBox_ is monitor via IMAP IDLE for new E-Mails and additionally also
scanned periodically. \
All mails in _HamMailbox_ are fed as Ham to rspamd and then moved to
_InboxMailbox_. The _HamMailbox_ is only processed periodically.

The mails that have been scanned last are stored in a state file.

## Configuration

rspamd-iscan can be configured via a TOML configuration file.
Example:

```toml
RspamdURL      = "http://192.168.178.2:11334"
RspamdPassword = "iwonttellyou"
ImapAddr       ="my-imap-server:993"
ImapUser       ="rickdeckard"
ImapPassword   ="zhora"
InboxMailbox   ="INBOX"
SpamMailbox    ="Spam"
HamMailbox     = "Ham"
ScanMailbox    ="Unscanned"
SpamThreshold = 10.0
```

The location of the configuration and state file can be specified via command
line parameters.
