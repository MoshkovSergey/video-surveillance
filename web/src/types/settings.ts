export interface SettingsDTO {
  telegram_enabled: boolean;
  telegram_chat_id: string;
  telegram_bot_token_set: boolean;
  telegram_bot_token_masked: string;
  telegram_events: string[];
}

export interface UpdateSettingsPayload {
  telegram_enabled?: boolean;
  telegram_chat_id?: string;
  telegram_bot_token?: string;
  telegram_events?: string[];
}

export interface TelegramTestPayload {
  telegram_bot_token?: string;
  telegram_chat_id?: string;
}