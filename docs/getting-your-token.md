# Getting your Moodle token

Zewail City Moodle signs you in with your university Office 365 account, so there's no Moodle password this tool could use (password sign-in through `login/token.php` always fails with `invalidlogin`). Instead it uses the same thing the official Moodle phone app uses: a **per-user web-service token**, about 32 letters and digits, tied to your account.

Pick either method. Both end with `moodle auth`.

## Method A: the app launch link (works with Office 365 sign-in)

1. Sign in to [moodle.zewailcity.edu.eg](https://moodle.zewailcity.edu.eg) in your browser.
2. In the same browser, open:

   ```text
   https://moodle.zewailcity.edu.eg/admin/tool/mobile/launch.php?service=moodle_mobile_app&passport=1&urlscheme=moodlemobile&confirmed=1
   ```

3. The page shows a link like *"Click here to launch the app"*. **Don't click it.** Right-click it and choose **Copy Link Address**. It starts with `moodlemobile://token=`.
4. Run `moodle auth` and paste the whole link at the hidden prompt. The tool decodes the token from it.

## Method B: Security keys page

1. Click your avatar (top right) → **Preferences** → **Security keys**, or go to [moodle.zewailcity.edu.eg/user/managetoken.php](https://moodle.zewailcity.edu.eg/user/managetoken.php).
2. Find the **Moodle mobile web service** row and click **Reset**.
3. Moodle shows the new key **once**, so copy it right away.
4. Run `moodle auth` and paste it. You won't see it as you type, which is normal.

> [!NOTE]
> **No "Moodle mobile web service" row?** Sign in to the Moodle phone app once (App Store / Google Play, search "Moodle", site `moodle.zewailcity.edu.eg`), then refresh the page.
>
> Resetting the key signs the phone app out. Just sign in to it again.

## Check it worked

```bash
moodle whoami
```

```json
{
  "fullname": "Your Name",
  "functions_available": 433,
  "release": "5.1.8 (Build: 20261005)",
  "site": "Zewail City Moodle",
  "username": "2023xxxxx@zewailcity.edu.eg"
}
```

Your Moodle username is your student ID email (`<id>@zewailcity.edu.eg`), not your `s-…` address.

To test a token before installing anything, ask Moodle directly:

```bash
TOKEN=paste_token_here
curl -s "https://moodle.zewailcity.edu.eg/webservice/rest/server.php" \
  --data-urlencode "wstoken=$TOKEN" \
  --data-urlencode "wsfunction=core_webservice_get_site_info" \
  --data-urlencode "moodlewsrestformat=json"
```

If it works, you get JSON back with `"sitename":"Zewail City Moodle"` and your name. If you get `"errorcode":"invalidtoken"`, get a new token.

## Where it's stored

The token is saved in `~/.config/moodle-mcp/config.json`, and only your user account can read the file. Set the `MOODLE_MCP_CONFIG` environment variable to keep it somewhere else, or set `MOODLE_TOKEN` to skip the file entirely.

```bash
moodle logout    # deletes the file
```

## Keep it safe

The token can do anything the Moodle app can do as you, including submitting work. Don't share it, commit it, or paste it into a chat. If you think it got out, press **Reset** on the Security keys page. The old token stops working right away. Then run `moodle auth` with the new one. Changing your Office 365 password does **not** revoke the token. Only Reset does.

Tokens also expire on their own. When a tool says `invalidtoken`, get a new one with Method A.
