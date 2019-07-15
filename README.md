# Joe - Postgres Query Optimization
Boost your backend development process


<div align="center">
    ![Joe Demo](./assets/demo.gif)
</div>

Provide developers access to experiment on automatically provisioned
production-size DB testing replica. Joe will provide recommendations
for query optimization and the ability to rollback.


## Install
### ZFS Store
Create a ZFS store (AWS EBS or GCP persistent disk) with a production Postgres
dump or archive (e.g. WAL-G archive). Specify its name and params in Joe
configuration (`config/provisioning.yaml`).

### Slack App
Configure a new Slack App in order to use Joe in Slack and add the app to your
team Workspace. Joe Bot should be available with public URL calls from Slack.
1. Create "#db-lab" channel in your Slack Workspace (You can use another channel name).
1. [Create a new Slack App](https://api.slack.com/apps?new_app=1).
    * Use "Joe Bot" as App Name and select a proper team Workspace.
1. Add Bot User.
    * Use "Joe Bot" as Display Name and "joe-bot" as the default username.
1. Run Joe Bot with `Bot User OAuth Access Token ("xoxb-TOKEN")` from "OAuth & Permissions" Feature and `Verification Token` from "Basic Information" page (See **Deploy** below).
1. Enable Incoming Webhooks Feature.
    * Press "Add New Webhook to Workspace" and select a previously created channel to post token.
1. Enable Event Subscriptions Feature.
    * Specify Request URL (URL will be verified by Slack API) (e.g. http://35.200.200.200:3000, https://joe.dev.domain.com). You would need to run Joe with proper settings before you could verify Request URL.
    * * Add `app_mention` and `message.channels` to "Subscribe to Bot Events".
1. Invite "Joe Bot" to "#db-lab" channel.

### Deploy
Deploy Joe instance in your infrastructure. You would need to:
1. Update configuration in `makerun.sh` and `config/provisioning.yaml`.
1. Make a publicly accessible HTTP server port specified in the configuration for Slack Events Request URL.
1. Build and run Joe `bash ./makerun.sh`.

Joe will automatically provision AWS EC2 or GCP GCE instance of Postgres.
