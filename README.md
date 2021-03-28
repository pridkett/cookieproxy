# CookieProxy

Patrick Wagstrom &lt;patrick@wagstrom.net&gt;

February 2021

## Overview

This was made for a _very_ niche use case of needing to use [telegraf](telegraf) with a remote API that required cookies and non-standard authentication method to get those cookies. Using this tool you can proxy through those requests with the appropriate cookies.

## Usage

While CookieProxy works without a CookieJar, you'll first want to create a CookieJar for maximum awesomeness.

```bash
./cookieproxy -cookiejar ~/cookies.txt
```

You'll see that CookieProxy has started on port 8675 and is ready to proxy requests:

```bash
curl http://localhost:8675/p/?target=http://foo.com/bar.png
```

### Advanced Usage

I recently added support for querying a host to grab the cookies. This is particularly useful for my main use case of acting as an authenticated proxy to a Tesla Powerwall. When using it in this way you don't need to specify the `-cookiejar` argument, but instead pass a JSON object as a string to the `-request` argument.

```bash
./cookieproxy -request '{"url": "https://powerwall/api/login/Basic", "headers": {"Content-Type": "application/json"}, "body": "{\\"username\\":\\"customer\\",\\"password\\":\\"YOUR_POWERWALL_PASSWORD\\",\\"force_sm_off\\":false}", "method": "POST"}'
```

Then you can easily validate it with the following command:

```bash
curl "http://localhost:8675/p/?target=https://powerwall/api/meters/aggregates"
```

## License

Copyright © 2021 Patrick Wagstrom

Licensed under terms of the MIT License
