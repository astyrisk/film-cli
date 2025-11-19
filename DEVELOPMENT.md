This document describes how to get the original streaming file from a protected source.

The target URL is: `https://vidsrc-embed.ru/embed/movie?imdb=tt1300854`

### Step 1: Fetch the Initial Page

First, we fetch the HTML of the movie's page. A local copy is available at [`example/index.html`](example/index.html).

Inside this HTML, we find an `<iframe>` element. The `src` attribute of this iframe contains a URL to an "rcp" file.

```html
<iframe id="player_iframe" src="//cloudnestra.com/rcp/YTUzODlhYjBhNTNhMDFj..." frameborder="0" scrolling="no" allowfullscreen="yes" style="height: 100%; width: 100%;"></iframe>
```

### Step 2: Fetch the RCP Page

Next, we fetch the HTML from the "rcp" URL found in the iframe.

It appears the "rcp" URL must be opened in a browser to generate the next URL.
We need to investigate the JavaScript responsible for this to automate it.

An example of the fetched "rcp" page is at [`example/iframe(rcp).html`](example/iframe(rcp).html).

### Step 3: Extract the Prorcp URL

Note: a certain JS has to be executed in the rcp file first.

The "rcp" page contains a JavaScript function called `loadIframe`. This function dynamically creates another iframe with a "prorcp" URL.

```javascript
function loadIframe(data = 1){
    if(data == 1){
        $("#the_frame").removeAttr("style");
        $("#the_frame").html("");
        $('<iframe>', {
           id: 'player_iframe',
           src: '/prorcp/OTk0NjBkMTNjNmNlYjZlMmE0...',
           frameborder: 0,
           scrolling: 'no',
           allowfullscreen: 'yes',
           allow: "autoplay",
           style: 'height: 100%; width: 100%;'
        }).appendTo('#the_frame');
        $("#player_iframe").on("load", function () {
            $("#the_frame").attr("style","background-image: none;");
        });
    }
}
```

We need to grab the `/prorcp/` URL and fetch it. It is important to set the referrer to `https://cloudnestra.com` for this request.

Here is an example using `curl`:
```bash
curl -e "https://cloudnestra.com" "https://cloudnestra.com/prorcp/N2M0YjgzN2IyM2I2MzRj..."
```

An example of the fetched "prorcp" page is at [`example/prorcp.html`](example/prorcp.html).

### Step 4: Get the Decoding Script and Encoded String

The "prorcp" page includes a script. We need to fetch this script.

```html
<script src="/sV05kUlNvOdOxvtC/bd228be1828c2944c66227cde1ebbbd3.js?_=1744906950"></script>
```

The script is located at: `https://cloudnestra.com/sV05kUlNvOdOxvtC/bd228be1828c2944c66227cde1ebbbd3.js`

We also need to extract an encoded string from the page. It is in a `div` element.

```html
<div id="xTyBxQyGTA" style="display:none;">A=AgBTDdEzE0UmTLO0BNXXUaFsQ9NSBQO...</div>
```

### Step 5: Decode the String

Finally, we can decode the string. Use the fetched JavaScript file and the encoded string. The decoding logic is demonstrated in [`decode.html`](decode.html).