 ### Describe a sequence of events that would result in a client reading stale data from the Google File System.


- client C1 asks the master server for a file
- the master responds with a list of chunkservers S1(primary), S2, S3
- at the same time, client C2 sends write requests to S1.
  - A disturbance happens in the network causing requests between S1 and S3 to fail. S1 and S3 can talk to the master so they are still considered alive.
- C1 chooses S3 to read the file from

result: depending on how writes failed between S1 and S3 (and how C2 handles retries), C1 may see incomplete or duplicate data. If all requests between S1 and S3 fail, C1 will read stale data.