import type { Greet } from "./util";
import type { Exclaim } from "exclaim";

type Text<Status extends number, Body extends string> = {
  status: Status;
  headers: { "content-type": "text/plain; charset=utf-8" };
  body: Body;
};

export type Handle<Req> = Req extends { method: "POST"; path: "/login" }
  ? {
      status: 200;
      headers: {
        "content-type": "text/plain; charset=utf-8";
        "set-cookie": ["session=abc", "visited=true"];
      };
      body: "ok\n";
    }
  : Req extends { method: "GET"; path: `/greet/${infer Name}` }
    ? Text<200, Exclaim<Greet<Name>>>
    : Req extends { method: "POST"; path: "/echo"; body: infer B extends string }
      ? Text<200, B>
      : Req extends { headers: { "x-magic": infer M extends string } }
        ? Text<200, `magic: ${M}`>
        : Text<404, "not found\n">;
