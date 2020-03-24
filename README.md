# Joe - Postgres Query Optimization
Boost your backend development process

<div align="center">
    ![Joe Demo](./assets/demo.gif)
</div>

Provide developers access to experiment on automatically provisioned
production-size DB testing replica. Joe will provide recommendations
for query optimization and the ability to rollback.

## Status

The project is in its early stage. However, it is already being extensively used
in some teams in their daily work. Since production is not involved, it is
quite easy to try and start using it.

Please support the project giving a GitLab star (it's on [the main page](https://gitlab.com/postgres-ai/joe),
at the upper right corner):

![Add a star](./assets/star.gif)

To discuss Joe, [join our community Slack](https://database-lab-team-slack-invite.herokuapp.com/).

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
1. Grant permissions on the "OAuth & Permissions" page for the following "Bot Token Scopes" :
    * `channels:history`
    * `chat:write`
    * `files:write`
    * `incoming-webhook`
    * `reactions:write`
    * `users.profile:read`
    * `users:read`
1. Run Joe Bot with `Bot User OAuth Access Token ("xoxb-TOKEN")` from "OAuth & Permissions" Feature and `Signing Secret` from "Basic Information" page (See **Run** below).
1. Enable Incoming Webhooks Feature.
    * Press "Add New Webhook to Workspace" and select a previously created channel to post token.
1. Enable Event Subscriptions Feature.
    * Specify Request URL (URL will be verified by Slack API) (e.g. http://35.200.200.200:3001, https://joe.dev.domain.com). You would need to run Joe with proper settings before you could verify Request URL.
    * Add `message.channels` to "Subscribe to Bot Events".
1. Invite "Joe Bot" to "#db-lab" channel.

### 3. Run
Deploy Joe instance in your infrastructure. You would need to:

1. Configure communication channels. You can copy the sample `config/config.sample.yml` to `~/.dblab/joe_configs/config.yml`, inspect all configuration options, and adjust if needed.
   
1. Run the Joe Docker image to connect with the Database Lab server according to the previous configurations. 
    Example:

    ```bash
    docker run \
      --name joe_bot \
      --publish 3001:3001 \
      --restart=on-failure \
      --volume ~/.dblab/joe_configs/config.yml:/home/configs/config.yml \
      --env SERVER_PORT=3001 \
      --detach \
      postgresai/joe:latest
    ``` 
    The Joe instance will be running by port 3001 of the current machine.
    
1. Make a publicly accessible HTTP(S) server port specified in the configuration for Slack Events Request URL.
1. Send a command to the #db-lab channel. For example, `help`.


## Development
See our [GitLab Container Registry](https://gitlab.com/postgres-ai/joe/container_registry) for develop builds. 

## Community

Bug reports, ideas, and merge requests are welcome: https://gitlab.com/postgres-ai/joe

To discuss Joe, join our Slack: https://database-lab-team-slack-invite.herokuapp.com/
