<a href="https://postgres.ai"><img src="https://img.shields.io/badge/Postgres-AI-orange.svg" alt="Postgres.AI" /></a>

# Joe Bot
<img width="200" src="https://gitlab.com/postgres-ai-team/joe/uploads/e49a12a9584b5d6fae2fb017e70b0995/Screen_Shot_2019-05-15_at_18.43.05.png" align="right">

Query optimization assistant.

### Build & Run
```bash
go build -o bin/joe 
./bin/joe \
  --host="localhost" \
  --dbname="db" \
  --token="xoxb-XXXXXX" \
  --verification-token="XXXXXX"
```

### Set Up a Slack App
In order to use Joe in Slack, you need to configure a new Slack App and add it to your Workspace. Joe Bot should be available with public URL calls from Slack.
1. Create "#db-lab" channel in your Slack Workspace (You can use another channel name)
2. [Create a new Slack App](https://api.slack.com/apps?new_app=1)
  * Use "Joe Bot" as App Name and select a proper Workspace
3. Add Bot User
  * Use "Joe Bot" as Display Name and "joe-bot" as the default username
4. Run Joe Bot with Access Token from "OAuth & Permissions" Feature and Verification Token from "Basic Information" page
5. Enable Incoming Webhooks Feature
  * Press "Add New Webhook to Workspace" and select a previously created channel to post token
6. Enable Event Subscriptions Feature
  * Specify Request URL (URL will be verified by Slack API)
  * Add "app_mention" and "message.channels" to "Subscribe to Bot Events"
7. Invite "Joe Bot" to "#db-lab" channel
