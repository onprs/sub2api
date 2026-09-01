import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import { createPinia } from "pinia";
import { createI18n } from "vue-i18n";
import type { SubscriptionPlan } from "@/types/payment";
import SubscriptionPlanCard from "../SubscriptionPlanCard.vue";

const i18n = createI18n({
  legacy: false,
  locale: "en",
  fallbackWarn: false,
  missingWarn: false,
  messages: {
    en: {
      payment: {
        days: "days",
        weeks: "weeks",
        months: "months",
        perMonth: "month",
        models: "Models",
        planCard: {
          dailyLimit: "Daily",
          monthlyLimit: "Monthly",
          quota: "Quota",
          rate: "Rate",
          weeklyLimit: "Weekly",
          unlimited: "Unlimited",
        },
        quotaWindows: {
          fiveHourShort: "5h",
          sevenDayShort: "7d",
          thirtyDayShort: "30d",
        },
        subscribeNow: "Subscribe now",
        renewNow: "Renew",
        renewalDiscount: "Renewal -{percent}%",
        stock: {
          unlimited: "Unlimited stock",
          available: "{count} remaining",
          availableMany: "{count} remaining",
          inStock: "In stock",
          soldOut: "Sold out",
        },
      },
    },
  },
});

const mountPlanCard = (groupPlatform: string, overrides: Partial<SubscriptionPlan> = {}) =>
  mount(SubscriptionPlanCard, {
    props: {
      plan: {
        id: 1,
        group_id: 10,
        group_platform: groupPlatform,
        name: "Pro",
        price: 10,
        amount: 1000,
        features: [],
        rate_multiplier: 1,
        validity_days: 30,
        validity_unit: "day",
        supported_model_scopes: ["claude", "gemini_text", "gemini_image"],
        is_active: true,
        ...overrides,
      },
    },
    global: { plugins: [i18n, createPinia()] },
  });

describe("SubscriptionPlanCard", () => {
  it("shows subscribe when only another plan in the same group is active", () => {
    const text = mount(SubscriptionPlanCard, {
      props: {
        plan: {
          id: 2,
          group_id: 10,
          group_platform: "openai",
          name: "Plan B",
          price: 10,
          amount: 1000,
          features: [],
          rate_multiplier: 1,
          validity_days: 30,
          validity_unit: "day",
          supported_model_scopes: [],
          is_active: true,
        },
        activeSubscriptions: [
          {
            id: 1,
            user_id: 1,
            group_id: 10,
            plan_id: 1,
            status: "active",
            starts_at: "2026-01-01T00:00:00Z",
            expires_at: "2026-02-01T00:00:00Z",
            five_hour_limit_usd: null,
            seven_day_limit_usd: null,
            thirty_day_limit_usd: null,
            five_hour_usage_usd: 0,
            seven_day_usage_usd: 0,
            thirty_day_usage_usd: 0,
            five_hour_window_start: null,
            seven_day_window_start: null,
            thirty_day_window_start: null,
            created_at: "2026-01-01T00:00:00Z",
            updated_at: "2026-01-01T00:00:00Z",
          },
        ],
      },
      global: { plugins: [i18n, createPinia()] },
    }).text();

    expect(text).toContain("payment.subscribeNow");
    expect(text).not.toContain("payment.renewNow");
  });

  it("shows renewal when the same plan is active", () => {
    const text = mount(SubscriptionPlanCard, {
      props: {
        plan: {
          id: 2,
          group_id: 10,
          group_platform: "openai",
          name: "Plan B",
          price: 10,
          amount: 1000,
          features: [],
          rate_multiplier: 1,
          validity_days: 30,
          validity_unit: "day",
          supported_model_scopes: [],
          is_active: true,
        },
        activeSubscriptions: [
          {
            id: 1,
            user_id: 1,
            group_id: 10,
            plan_id: 2,
            status: "active",
            starts_at: "2026-01-01T00:00:00Z",
            expires_at: "2026-02-01T00:00:00Z",
            five_hour_limit_usd: null,
            seven_day_limit_usd: null,
            thirty_day_limit_usd: null,
            five_hour_usage_usd: 0,
            seven_day_usage_usd: 0,
            thirty_day_usage_usd: 0,
            five_hour_window_start: null,
            seven_day_window_start: null,
            thirty_day_window_start: null,
            created_at: "2026-01-01T00:00:00Z",
            updated_at: "2026-01-01T00:00:00Z",
          },
        ],
      },
      global: { plugins: [i18n, createPinia()] },
    }).text();

    expect(text).toContain("payment.renewNow");
  });

  it("shows discounted renewal price only when the same plan is renewal eligible", () => {
    const text = mount(SubscriptionPlanCard, {
      props: {
        plan: {
          id: 2,
          group_id: 10,
          group_platform: "openai",
          name: "Plan B",
          price: 8.7,
          effective_price: 7.4,
          renewal_price: 7.4,
          renewal_discount_percent: 15,
          renewal_eligible: true,
          amount: 1000,
          features: [],
          rate_multiplier: 1,
          validity_days: 30,
          validity_unit: "day",
          supported_model_scopes: [],
          is_active: true,
        },
        activeSubscriptions: [
          {
            id: 1,
            user_id: 1,
            group_id: 10,
            plan_id: 2,
            status: "active",
            starts_at: "2026-01-01T00:00:00Z",
            expires_at: "2026-02-01T00:00:00Z",
            five_hour_limit_usd: null,
            seven_day_limit_usd: null,
            thirty_day_limit_usd: null,
            five_hour_usage_usd: 0,
            seven_day_usage_usd: 0,
            thirty_day_usage_usd: 0,
            five_hour_window_start: null,
            seven_day_window_start: null,
            thirty_day_window_start: null,
            created_at: "2026-01-01T00:00:00Z",
            updated_at: "2026-01-01T00:00:00Z",
          },
        ],
      },
      global: { plugins: [i18n, createPinia()] },
    }).text();

    expect(text).toContain("7.4");
    expect(text).toContain("$8.7");
    expect(text).toContain("payment.renewalDiscount");
  });

  it("does not show renewal discount for another plan in the same group", () => {
    const text = mount(SubscriptionPlanCard, {
      props: {
        plan: {
          id: 2,
          group_id: 10,
          group_platform: "openai",
          name: "Plan B",
          price: 8.7,
          effective_price: 8.7,
          renewal_price: null,
          renewal_discount_percent: 15,
          renewal_eligible: false,
          amount: 1000,
          features: [],
          rate_multiplier: 1,
          validity_days: 30,
          validity_unit: "day",
          supported_model_scopes: [],
          is_active: true,
        },
        activeSubscriptions: [
          {
            id: 1,
            user_id: 1,
            group_id: 10,
            plan_id: 1,
            status: "active",
            starts_at: "2026-01-01T00:00:00Z",
            expires_at: "2026-02-01T00:00:00Z",
            five_hour_limit_usd: null,
            seven_day_limit_usd: null,
            thirty_day_limit_usd: null,
            five_hour_usage_usd: 0,
            seven_day_usage_usd: 0,
            thirty_day_usage_usd: 0,
            five_hour_window_start: null,
            seven_day_window_start: null,
            thirty_day_window_start: null,
            created_at: "2026-01-01T00:00:00Z",
            updated_at: "2026-01-01T00:00:00Z",
          },
        ],
      },
      global: { plugins: [i18n, createPinia()] },
    }).text();

    expect(text).toContain("8.7");
    expect(text).not.toContain("7.4");
    expect(text).not.toContain("Renewal -15%");
  });

  it("does not show Antigravity model scopes for OpenAI plans", () => {
    const text = mountPlanCard("openai").text();

    expect(text).not.toContain("Claude");
    expect(text).not.toContain("Gemini");
    expect(text).not.toContain("Imagen");
  });

  it("shows model scopes for Antigravity plans", () => {
    const text = mountPlanCard("antigravity").text();

    expect(text).toContain("Claude");
    expect(text).toContain("Gemini");
    expect(text).toContain("Imagen");
  });

  it("shows rolling quota windows instead of legacy group limits", () => {
    const text = mount(SubscriptionPlanCard, {
      props: {
        plan: {
          id: 2,
          group_id: 10,
          group_platform: "openai",
          name: "Rolling",
          price: 10,
          amount: 1000,
          features: [],
          rate_multiplier: 1,
          validity_days: 30,
          validity_unit: "day",
          daily_limit_usd: 99,
          weekly_limit_usd: 9999,
          monthly_limit_usd: 9999,
          five_hour_limit_usd: 1,
          seven_day_limit_usd: 7,
          thirty_day_limit_usd: 30,
          supported_model_scopes: [],
          is_active: true,
        },
      },
      global: { plugins: [i18n, createPinia()] },
    }).text();

    expect(text).toContain("5h");
    expect(text).toContain("$1");
    expect(text).toContain("7d");
    expect(text).toContain("$7");
    expect(text).toContain("30d");
    expect(text).toContain("$30");
    expect(text).not.toContain("$99");
    expect(text).not.toContain("$9999");
  });

  it("shows plural validity units from normalized plan data", () => {
    const text = mount(SubscriptionPlanCard, {
      props: {
        plan: {
          id: 3,
          group_id: 10,
          group_platform: "openai",
          name: "Two Weeks",
          price: 10,
          amount: 1000,
          features: [],
          rate_multiplier: 1,
          validity_days: 2,
          validity_unit: "weeks",
          supported_model_scopes: [],
          is_active: true,
        },
      },
      global: { plugins: [i18n, createPinia()] },
    }).text();

    expect(text).toContain("2weeks");
    expect(text).not.toContain("2days");
  });

  it("shows unlimited stock for plans without a stock limit", () => {
    const text = mount(SubscriptionPlanCard, {
      props: {
        plan: {
          id: 4,
          group_id: 10,
          group_platform: "openai",
          name: "Unlimited Stock",
          price: 10,
          features: [],
          rate_multiplier: 1,
          validity_days: 30,
          validity_unit: "day",
          for_sale: true,
          sort_order: 1,
        },
      },
      global: { plugins: [i18n, createPinia()] },
    }).text();

    expect(text).toContain("payment.stock.unlimited");
  });

  it("shows remaining stock for positive stock values", () => {
    const text = mount(SubscriptionPlanCard, {
      props: {
        plan: {
          id: 5,
          group_id: 10,
          group_platform: "openai",
          name: "Limited Stock",
          price: 10,
          stock: 3,
          features: [],
          rate_multiplier: 1,
          validity_days: 30,
          validity_unit: "day",
          for_sale: true,
          sort_order: 1,
        },
      },
      global: { plugins: [i18n, createPinia()] },
    }).text();

    expect(text).toContain("payment.stock.remaining");
  });

  it("disables sold-out plans and does not emit select", async () => {
    const wrapper = mount(SubscriptionPlanCard, {
      props: {
        plan: {
          id: 6,
          group_id: 10,
          group_platform: "openai",
          name: "Sold Out",
          price: 10,
          stock: 0,
          sold_out: true,
          features: [],
          rate_multiplier: 1,
          validity_days: 30,
          validity_unit: "day",
          for_sale: true,
          sort_order: 1,
        },
      },
      global: { plugins: [i18n, createPinia()] },
    });

    const button = wrapper.find("button");
    expect(button.attributes("disabled")).toBeDefined();
    expect(button.text()).toContain("payment.stock.soldOut");

    await button.trigger("click");

    expect(wrapper.emitted("select")).toBeUndefined();
  });

  it("renders normalized month, week, and day validity units", () => {
    expect(mountPlanCard("openai", { validity_days: 1, validity_unit: "months" }).text()).toContain("/ month");
    expect(mountPlanCard("openai", { validity_days: 3, validity_unit: "months" }).text()).toContain("/ 3months");
    expect(mountPlanCard("openai", { validity_days: 2, validity_unit: "weeks" }).text()).toContain("/ 2weeks");
    expect(mountPlanCard("openai", { validity_days: 30, validity_unit: "day" }).text()).toContain("/ 30days");
  });

  it("uses the configured currency symbol while preserving USD for legacy plans", () => {
    const cnyPlan = mountPlanCard("openai", { currency: "CNY", original_price: 20 }).text();

    expect(cnyPlan).toContain("¥10CNY");
    expect(cnyPlan).toContain("¥20CNY");
    expect(mountPlanCard("openai", { currency: "USD" }).text()).toContain("$10USD");
    expect(mountPlanCard("openai", { currency: "" }).text()).toContain("$10");
  });
});
