"""
# How to use

It is a mitmproxy addon, you'll need to use uv because it require an external library to work (warcio)
You can launch it with `uvx --with warcio --from mitmproxy mitmweb -s .\addon.py` 
And use FoxyProxy to use it as a proxy
"""


from warcio.archiveiterator import ArchiveIterator
from mitmproxy import http


class WARC:
    def __init__(self):
        self.records = {}

    def parse_warc(self, path):
        with open(path, "rb") as stream:
            for record in ArchiveIterator(stream):
                if record.rec_type == "response":
                    if record.rec_type != "response":
                        continue
                    
                    uri = record.rec_headers.get_header("WARC-Target-URI")
                    if not uri:
                        continue
                    
                    status_str = record.http_headers.get_statuscode()
                    try:
                        status = int(status_str)
                    except:
                        status = 200
                        
                    headers= {}
                    for name, value in record.http_headers.headers:
                        lname = name.lower()
                        if lname in ("transfer-encoding", "content-length", "connection"):
                            continue
                        headers[name] = value
                        
                    try:
                        body = record.content_stream().read()
                    except:
                        body = b"Unable to read body"
                        
                    self.records[uri] = (status, headers, body)

    def request(self, flow):
        url = flow.request.url
        resp = self.records.get(url)

        if resp is None:
            return

        status, headers, body = resp

        flow.response = http.Response.make(
            status_code=status, content=body, headers=headers
        )

        flow.metadata["replayed_from_warc"] = True
        flow.response.headers["X-Replayed-From-WARC"] = "true"


addon = WARC()
addon.parse_warc("../archive.gz")
addons = [addon]
