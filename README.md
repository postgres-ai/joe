# Joe - Postgres Query Optimization
Boost your backend development process

<div align="center">
    ![Joe Demo](./assets/demo.gif)
</div>

Provide developers access to experiment on automatically provisioned
production-size DB testing replica. Joe will provide recommendations
for query optimization and the ability to rollback.

## Install Software

### 1. Database Lab
Install and setup [Database Lab](https://gitlab.com/postgres-ai/database-lab) 

### 2. Slack App
Configure a new Slack App in order to use Joe in Slack and add the app to your
team Workspace. Joe Bot should be available with public URL calls from Slack.
1. Create "#db-lab" channel in your Slack Workspace (You can use another channel name).
1. [Create a new Slack App](https://api.slack.com/apps?new_app=1).
    * Use "Joe Bot" as App Name and select a proper team Workspace.
1. Add Bot User.
    * Use "Joe Bot" as Display Name and "joe-bot" as the default username.
1. Run Joe Bot with `Bot User OAuth Access Token ("xoxb-TOKEN")` from "OAuth & Permissions" Feature and `Verification Token` from "Basic Information" page (See **Run** below).
1. Enable Incoming Webhooks Feature.
    * Press "Add New Webhook to Workspace" and select a previously created channel to post token.
1. Enable Event Subscriptions Feature.
    * Specify Request URL (URL will be verified by Slack API) (e.g. http://35.200.200.200:3000, https://joe.dev.domain.com). You would need to run Joe with proper settings before you could verify Request URL.
    * * Add `app_mention` and `message.channels` to "Subscribe to Bot Events".
1. Invite "Joe Bot" to "#db-lab" channel.

### 3. Run
Deploy Joe instance in your infrastructure. You would need to:

1. Run the Joe Docker image to connect with the Database Lab server according to the previous configurations. 
    Example:

    ```bash
    docker run \
    --env DBLAB_URL="https://dblab.domain.com" \
    --env DBLAB_TOKEN="DBLAB_SECRET_TOKEN" \
    --env CHAT_TOKEN="YOUR_SLACK_CHAT_TOKEN" \
    --env CHAT_VERIFICATION_TOKEN="YOUR_SLACK_VERIFICATION_TOKEN" \
    --env SERVER_PORT=3000 \
    -p 3000:3000 \
    postgresai/joe:latest
    ``` 
    The Joe instance will be running by port 3000 of the current machine.
    
1. Make a publicly accessible HTTP(S) server port specified in the configuration for Slack Events Request URL.
1. Send a command to the #db-lab channel. For example, `\d`.


## Development
See our [GitLab Container Registry](https://gitlab.com/postgres-ai/joe/container_registry) for develop builds. 

## Community

Bug reports, ideas, and merge requests are welcome: https://gitlab.com/postgres-ai/joe

To discuss Joe, join our Slack: https://database-lab-team-slack-invite.herokuapp.com/
