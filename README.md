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
 
Prepare one or more Database Lab instances before configuring Joe bot.

Then, configure ways of communication with Joe.

### 2. Configure communication channels

There are two available types of communication:
- Web UI powered by [Postgres.ai Console](https://postgres.ai/console/)
- Slack

You can use both of them in parallel. Feel free to implement more types of communication: see [communication channels issues](https://gitlab.com/postgres-ai/joe/-/issues?label_name%5B%5D=Communication+channel).

### 2a. Set up Joe in Postgres.ai Console ("Web UI")
If you don't need Web UI and prefer working with Joe only in messengers (such as Slack), proceed to the next step.

To configure Web UI:

1. First, get your `PLATFORM_TOKEN`. In [Postgres.ai Console](https://postgres.ai/console/), switch to proper organization and open the `Access Tokens` page.
1. Then, go to the `Joe instances` page from the `SQL Optimization` sidebar section, choose a project from the dropdown menu and `Add instance`.
1. Generate `Signing secret`. Use the secret as `WEBUI_SIGNING_SECRET` at the configuration file. We will add and verify the URL on the last step.


### 2b. Slack App
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
    * `files:read`
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

### 3. Run
Deploy Joe instance in your infrastructure. You would need to:

1. Configure communication channels. You can copy the sample `config/config.sample.yml` to `~/.dblab/configs/joe_config.yml`, inspect all configuration options, and adjust if needed.
   
1. Run the Joe Docker image to connect with the Database Lab server according to the previous configurations. 
    Example:

    ```bash
    docker run \
      --name joe_bot \
      --publish 3001:3001 \
      --env SERVER_PORT=3001 \
      --volume ~/.dblab/configs/joe_config.yml:/home/config/config.yml \
      --restart=on-failure \
      --detach \
      postgresai/joe:latest
    ``` 
    The Joe instance will be running by port 3001 of the current machine.
    
1. Make a publicly accessible HTTP(S) server port specified in the configuration for Web UI/Slack Events Request URL.

### 4. Verify the configuration

### 4a. Finish the WebUI configuration

1. Return to the page of Joe configuration in the Console, enter the URL with the specific path `/webui/`. For example, `https://joe.dev.domain.com/webui/`.
1. Press the `Verify` button to check connection and `Add` the instance after the verification is passed.
1. Choose the created instance and send a command.


### 4b. Finish the Slack App configuration
1. Enable Event Subscriptions Feature.
    * Go to the "Event Subscriptions" page.
    * Specify Request URL adding the specific for connection path: `/slack/` (URL will be verified by Slack API). You would need to run Joe with proper settings before you could verify Request URL. For example, `https://joe.dev.domain.com/slack/`
    * In the "Subscribe to Bot Events" dropdown-tab add `message.channels`.
    * Press "Save Changes".

1. Invite "Joe Bot" to "#db-lab" channel.
1. Send a command to the #db-lab channel. For example, `help`.


## Development
See our [GitLab Container Registry](https://gitlab.com/postgres-ai/joe/container_registry) for develop builds. 

## Community

Bug reports, ideas, and merge requests are welcome: https://gitlab.com/postgres-ai/joe

To discuss Joe, join our Slack: https://database-lab-team-slack-invite.herokuapp.com/
