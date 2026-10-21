//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestCreateOrderInTx_PersistsRechargeBonusAmounts(t *testing.T) {
	for _, tc := range []struct {
		mode   string
		amount float64
		pay    float64
		bonus  float64
	}{
		{mode: RechargeBonusModeBonus, amount: 12, pay: 10.5, bonus: 2},
		{mode: RechargeBonusModeDiscount, amount: 10, pay: 8.4, bonus: 2},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			user, err := client.User.Create().
				SetEmail("recharge-snapshot@example.invalid").
				SetPasswordHash("hash").
				SetUsername("recharge-snapshot").
				Save(ctx)
			require.NoError(t, err)

			cfg := &PaymentConfig{
				BalanceRechargeMultiplier: 1,
				RechargeBonusMode:         tc.mode,
				RechargeBonusTiers:        []RechargeBonusTier{{MinAmount: 10, BonusPercent: 20}},
				RechargeFeeRate:           5,
				MaxPendingOrders:          3,
				OrderTimeoutMin:           30,
			}
			quote := quoteRechargeBonus(cfg, 10, "USD")
			_, payAmount, err := calculateCreateOrderPayAmountForOrderType(
				quote.PayBase, cfg.RechargeFeeRate, "USD", payment.OrderTypeBalance, 0,
			)
			require.NoError(t, err)

			svc := &PaymentService{entClient: client}
			order, err := svc.createOrderInTx(ctx, CreateOrderRequest{
				UserID:      user.ID,
				Amount:      10,
				PaymentType: payment.TypeStripe,
				OrderType:   payment.OrderTypeBalance,
			}, &User{ID: user.ID, Email: user.Email, Username: user.Username}, nil, cfg,
				quote.Credited, quote.PayBase, cfg.RechargeFeeRate, payAmount, quote.Bonus,
				&payment.InstanceSelection{ProviderKey: payment.TypeStripe, Config: map[string]string{"currency": "USD"}},
			)
			require.NoError(t, err)
			stored, err := client.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, tc.amount, stored.Amount)
			require.Equal(t, tc.pay, stored.PayAmount)
			require.Equal(t, tc.bonus, stored.BonusAmount)
			require.Equal(t, 5.0, stored.FeeRate)
			require.Equal(t, "USD", stored.ProviderSnapshot["currency"])

			// Later promotion changes must not alter the saved monetary snapshot.
			cfg.RechargeBonusTiers = nil
			cfg.RechargeBonusMode = RechargeBonusModeBonus
			cfg.BalanceRechargeMultiplier = 7
			require.NotEqual(t, tc.amount, quoteRechargeBonus(cfg, 10, "USD").Credited)
			reread, err := client.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, stored.Amount, reread.Amount)
			require.Equal(t, stored.PayAmount, reread.PayAmount)
			require.Equal(t, stored.BonusAmount, reread.BonusAmount)
		})
	}
}
