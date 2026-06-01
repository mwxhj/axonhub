import { ApiFormat } from '@/features/channels/data/schema';
import { CHANNEL_CONFIGS } from '@/features/channels/data/config_channels';

export type ChannelType = keyof typeof CHANNEL_CONFIGS;

export interface CurlGeneratorOptions {
  headers?: Record<string, any>;
  body?: any;
  baseUrl?: string;
  apiFormat?: ApiFormat;
  channelType?: ChannelType;
}

const SENSITIVE_HEADER_RE = /(authorization|api[-_]?key|subscription[-_]?key|secret|token|cookie|www-authenticate|proxy-authorization)/i;
const SENSITIVE_BODY_KEY_RE = /^(api[-_]?key|access[-_]?token|refresh[-_]?token|id[-_]?token|client[-_]?secret|authorization|password|secret)$/i;
const BODY_BEARER_TOKEN_RE = /(bearer[\s:=]+)[a-zA-Z0-9_\-.]+/gi;
const BODY_SECRET_PAIR_RE = /(["']?(?:api[-_]?key|access[-_]?token|refresh[-_]?token|id[-_]?token|client[-_]?secret|authorization|password|secret)["']?\s*[:=]\s*)(["']?)([^"',\s}{\]]{4,})(["']?)/gi;

function isSensitiveHeader(key: string): boolean {
  return SENSITIVE_HEADER_RE.test(key);
}

export function maskSensitiveHeaders(headers: any): any {
  if (!headers || typeof headers !== 'object' || Array.isArray(headers)) {
    return headers;
  }

  return Object.entries(headers).reduce<Record<string, any>>((acc, [key, value]) => {
    acc[key] = isSensitiveHeader(key) ? (Array.isArray(value) ? ['******'] : '******') : value;
    return acc;
  }, {});
}

export function maskSensitiveBody(body: any): any {
  if (body == null) {
    return body;
  }

  if (typeof body === 'string') {
    return body.replace(BODY_BEARER_TOKEN_RE, '$1[REDACTED]').replace(BODY_SECRET_PAIR_RE, '$1$2[REDACTED]$4');
  }

  if (Array.isArray(body)) {
    return body.map((item) => maskSensitiveBody(item));
  }

  if (typeof body === 'object') {
    return Object.entries(body).reduce<Record<string, any>>((acc, [key, value]) => {
      acc[key] = SENSITIVE_BODY_KEY_RE.test(key) ? '[REDACTED]' : maskSensitiveBody(value);
      return acc;
    }, {});
  }

  return body;
}

const API_FORMAT_PATHS: Record<ApiFormat, string> = {
  'openai/chat_completions': '/v1/chat/completions',
  'openai/responses': '/v1/responses',
  'openai/image_generation': '/v1/images/generations',
  'openai/image_edit': '/v1/images/edits',
  'openai/image_variation': '/v1/images/variations',
  'openai/embeddings': '/v1/embeddings',
  'anthropic/messages': '/v1/messages',
  'gemini/contents': '/v1beta/models/{model}:generateContent',
  'gemini/embeddings': '/v1beta/models/{model}:embedContent',
  'aisdk/text': '/api/chat',
  'aisdk/datastream': '/api/datastream',
  'jina/rerank': '/v1/rerank',
  'jina/embeddings': '/jina/v1/embeddings',
  'ollama/chat': '/api/chat',
};

function getApiPath(apiFormat?: ApiFormat, body?: any, channelType?: ChannelType): string {
  if (!apiFormat) {
    return '/v1/chat/completions';
  }

  let path = API_FORMAT_PATHS[apiFormat] || '/v1/chat/completions';

  if (apiFormat === 'gemini/contents' && body?.model) {
    if (channelType === 'gemini_vertex') {
      path = '/v1/publishers/google/models/{model}:generateContent';
    }
    path = path.replace('{model}', body.model);
  }

  return path;
}

function getApiFormatFromChannelType(channelType?: ChannelType): ApiFormat | undefined {
  if (!channelType) return undefined;
  return CHANNEL_CONFIGS[channelType]?.apiFormat;
}

export function generateCurlCommand(options: CurlGeneratorOptions): string {
  const { headers, body, baseUrl, apiFormat, channelType } = options;

  const resolvedApiFormat = apiFormat || getApiFormatFromChannelType(channelType);
  const apiPath = getApiPath(resolvedApiFormat, body, channelType);

  let url: string;
  if (baseUrl) {
    const cleanBaseUrl = baseUrl.replace(/\/+$/, '');
    // Avoid path duplication: if baseUrl ends with a prefix of apiPath, strip the overlap.
    // e.g. baseUrl="https://api.openai.com/v1" + apiPath="/v1/chat/completions"
    //   -> "https://api.openai.com/v1/chat/completions" (not .../v1/v1/chat/completions)
    let combinedPath = apiPath;
    for (let i = 1; i <= apiPath.length; i++) {
      const prefix = apiPath.substring(0, i);
      if (cleanBaseUrl.endsWith(prefix)) {
        combinedPath = apiPath.substring(i);
      }
    }
    url = `${cleanBaseUrl}${combinedPath}`;
  } else {
    url = `${typeof window !== 'undefined' ? window.location.origin : ''}${apiPath}`;
  }

  const curlParts = [`curl '${url}'`];

  if (headers && typeof headers === 'object') {
    const skipHeaders = ['content-length', 'host', 'connection', 'accept-encoding', 'transfer-encoding'];
    Object.entries(headers).forEach(([key, value]) => {
      if (!skipHeaders.includes(key.toLowerCase()) && value) {
        const safeValue = isSensitiveHeader(key) ? '******' : value;
        const headerValue = String(safeValue).replace(/'/g, "'\\''");
        curlParts.push(`  -H '${key}: ${headerValue}'`);
      }
    });
  }

  const safeBody = maskSensitiveBody(body);
  if (safeBody) {
    const bodyStr = typeof safeBody === 'string' ? safeBody : JSON.stringify(safeBody);
    const escapedBody = bodyStr.replace(/'/g, "'\\''");
    curlParts.push(`  -d '${escapedBody}'`);
  }

  return curlParts.join(' \\\n');
}

export function generateRequestCurl(headers: any, body: any, apiFormat?: ApiFormat): string {
  return generateCurlCommand({
    headers,
    body: maskSensitiveBody(body),
    apiFormat: apiFormat || 'openai/chat_completions',
  });
}

export function generateExecutionCurl(
  headers: any,
  body: any,
  channel?: { baseURL?: string; type?: ChannelType },
  apiFormat?: ApiFormat
): string {
  return generateCurlCommand({
    headers,
    body: maskSensitiveBody(body),
    baseUrl: channel?.baseURL,
    channelType: channel?.type,
    apiFormat,
  });
}
