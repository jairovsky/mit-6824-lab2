### Could a received InstallSnapshot RPC cause the state machine to go backwards in time? That is, could step 8 in Figure 13 cause the state machine to be reset so that it reflects fewer executed operations? If yes, explain how this could happen. If no, explain why it can't happen.

No. For the state machine to go back in time, it would mean:

```
                       Sends RPC
         (server A)      ---->            (server B)
InstallSnapshot.lastIncludedIndex    <    lastApplied
```

It means server B already commited and applied entries that are newer
than the snapshot being sent, contradicting the need to install this snapshot.