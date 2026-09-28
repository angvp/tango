# Cookies' Secure flag trusts the proxy's forwarded protocol

`admin` and `accounts` set `Secure` on their cookies only when the request arrived over TLS, so local development over plain HTTP keeps working. Most deployments terminate TLS in a proxy or platform load balancer, though, and the app then sees plain HTTP: every cookie went out without `Secure`, even though visitors only ever used HTTPS.

`security.IsHTTPS` (exported as `auth.IsHTTPS`) now also counts a request as HTTPS when `X-Forwarded-Proto` says so, or, without that header, when `Forwarded` has `proto=https`. The first hop in either header wins.

Trusting these headers without a trusted-proxy list is normally a mistake (`ratelimit.RemoteIPKey` needs one for `X-Forwarded-For`). It's safe here because the flag only adds a restriction. A client that forges `https` gets a cookie its own browser won't send back over plain HTTP, and it can't forge its way to a weaker cookie. So there's nothing to configure, and the flag is right by default on the platforms that set the header. `IsHTTPS` must not be used for decisions a forged header could weaken, such as redirecting away from HTTPS or trusting a request's origin.
