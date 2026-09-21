/** A compact, dependency-free client for /api/agent/v1. */

export type HttpMethod = "GET" | "POST" | "PUT" | "PATCH" | "DELETE";

export type JsonRecord = Record<string, unknown>;

export interface LearningCompassClientOptions {
  /** e.g. http://macmini:8080 */
  baseUrl: string;
  /** The value configured as automation.api_token. */
  token: string;
  fetch?: typeof globalThis.fetch;
}

export interface RequestOptions extends Omit<RequestInit, "body" | "headers" | "method"> {
  body?: unknown;
  headers?: HeadersInit;
}

export interface Capability {
  name: string;
  description: string;
  operations: string[];
}

export interface CapabilityIndex {
  version: string;
  authentication: "Bearer token";
  capabilities: Capability[];
}

export class LearningCompassAPIError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly body?: unknown,
  ) {
    super(message);
    this.name = "LearningCompassAPIError";
  }
}

function isNativeBody(value: unknown): value is BodyInit {
  return value instanceof FormData || value instanceof URLSearchParams || value instanceof Blob ||
    value instanceof ArrayBuffer || ArrayBuffer.isView(value) || typeof value === "string";
}

/**
 * One deep interface for the complete Agent API. Domain helpers below cover
 * frequent workflows; request() remains available for every advertised route.
 */
export class LearningCompassClient {
  private readonly baseUrl: string;
  private readonly fetcher: typeof globalThis.fetch;

  constructor(private readonly options: LearningCompassClientOptions) {
    this.baseUrl = options.baseUrl.replace(/\/$/, "");
    this.fetcher = options.fetch ?? globalThis.fetch;
  }

  async request<T>(method: HttpMethod, path: string, options: RequestOptions = {}): Promise<T> {
    const headers = new Headers(options.headers);
    headers.set("Accept", "application/json");
    headers.set("Authorization", `Bearer ${this.options.token}`);

    let body: BodyInit | undefined;
    if (options.body !== undefined) {
      if (isNativeBody(options.body)) {
        body = options.body;
      } else {
        headers.set("Content-Type", "application/json");
        body = JSON.stringify(options.body);
      }
    }

    const normalizedPath = path.startsWith("/") ? path : `/${path}`;
    const response = await this.fetcher(`${this.baseUrl}/api/agent/v1${normalizedPath}`, {
      ...options,
      method,
      headers,
      body,
    });
    const contentType = response.headers.get("content-type") ?? "";
    const payload: unknown = contentType.includes("application/json")
      ? await response.json()
      : await response.text();

    if (!response.ok) {
      const message = typeof payload === "object" && payload && "error" in payload
        ? String((payload as JsonRecord).error)
        : `Learning Compass request failed (${response.status})`;
      throw new LearningCompassAPIError(message, response.status, payload);
    }
    return payload as T;
  }

  get<T>(path: string, options?: RequestOptions) { return this.request<T>("GET", path, options); }
  post<T>(path: string, body?: unknown, options?: RequestOptions) { return this.request<T>("POST", path, { ...options, body }); }
  put<T>(path: string, body?: unknown, options?: RequestOptions) { return this.request<T>("PUT", path, { ...options, body }); }
  patch<T>(path: string, body?: unknown, options?: RequestOptions) { return this.request<T>("PATCH", path, { ...options, body }); }
  delete<T>(path: string, options?: RequestOptions) { return this.request<T>("DELETE", path, options); }

  health() { return this.get<{ ok: boolean; ai: string[]; agentAPI: true }>("/health"); }
  capabilities() { return this.get<CapabilityIndex>("/capabilities"); }

  math = {
    sessions: () => this.get<JsonRecord>("/math/sessions"),
    saveSession: (session: JsonRecord) => this.post<JsonRecord>("/math/sessions", session),
    wrongs: (category = "practice") => this.get<JsonRecord>(`/math/wrongs?category=${encodeURIComponent(category)}`),
    updateWrong: (wrong: JsonRecord) => this.put<JsonRecord>("/math/wrongs", wrong),
    weakChallengeFacts: () => this.get<JsonRecord>("/math/challenge/weak"),
  };

  english = {
    weeks: () => this.get<JsonRecord>("/english/weeks"),
    week: (weekId: string) => this.get<JsonRecord>(`/english/weeks/${encodeURIComponent(weekId)}`),
    updateSpelling: (weekId: string, update: JsonRecord) => this.put<JsonRecord>(`/english/weeks/${encodeURIComponent(weekId)}/spell`, update),
    setFridayMistakes: (weekId: string, words: string[]) => this.put<JsonRecord>(`/english/weeks/${encodeURIComponent(weekId)}/friday-spelling`, { words }),
    generateWeek: (input: JsonRecord) => this.post<JsonRecord>("/english/weeks/generate/text", input),
  };

  chinese = {
    weeks: () => this.get<JsonRecord>("/chinese/weeks"),
    createWeek: (week: JsonRecord) => this.post<JsonRecord>("/chinese/weeks", week),
    dueReview: () => this.get<JsonRecord>("/chinese/review/due"),
    recordReview: (review: JsonRecord) => this.put<JsonRecord>("/chinese/review", review),
  };

  curriculum = {
    topics: () => this.get<JsonRecord>("/curriculum/topics"),
    topic: (code: string) => this.get<JsonRecord>(`/curriculum/topics/${encodeURIComponent(code)}`),
    mainLesson: () => this.get<JsonRecord>("/learning-plan/main-lesson"),
    setMainLesson: (input: { mode: "auto" | "manual"; topicCode?: string }) => this.put<JsonRecord>("/learning-plan/main-lesson", input),
  };

  reading = {
    books: (profileId = "learner") => this.get<JsonRecord>(`/reading/books?profileId=${encodeURIComponent(profileId)}`),
    createBook: (book: JsonRecord) => this.post<JsonRecord>("/reading/books", book),
    recordSession: (session: JsonRecord) => this.post<JsonRecord>("/reading/sessions", session),
    weeklyReview: (profileId = "learner", weekStart?: string) => this.get<JsonRecord>(`/reading/weekly-review?profileId=${encodeURIComponent(profileId)}${weekStart ? `&weekStart=${encodeURIComponent(weekStart)}` : ""}`),
  };

  piano = {
    weeks: () => this.get<JsonRecord>("/piano/weeks"),
    week: (weekId: number) => this.get<JsonRecord>(`/piano/weeks/${weekId}`),
    createWeek: (input: JsonRecord) => this.post<JsonRecord>("/piano/weeks", input),
    today: () => this.get<JsonRecord>("/piano/today"),
    addTask: (weekId: number, input: JsonRecord) => this.post<JsonRecord>(`/piano/weeks/${weekId}/tasks`, input),
    updateTask: (taskId: number, input: JsonRecord) => this.patch<JsonRecord>(`/piano/tasks/${taskId}`, input),
    attachSource: (weekId: number, file: Blob, filename: string, kind: "assignment" | "feedback" = "assignment", taskId?: number) => {
      const form = new FormData();
      form.append("file", file, filename);
      const query = taskId ? `&taskId=${encodeURIComponent(String(taskId))}` : "";
      return this.post<JsonRecord>(`/piano/weeks/${weekId}/assets?kind=${kind}${query}`, form);
    },
    recordSession: (weekId: number, input: JsonRecord) => this.post<JsonRecord>(`/piano/weeks/${weekId}/sessions`, input),
    recordFeedback: (weekId: number, input: JsonRecord) => this.post<JsonRecord>(`/piano/weeks/${weekId}/feedback`, input),
  };
}
