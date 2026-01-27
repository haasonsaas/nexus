#import <Foundation/Foundation.h>

NS_ASSUME_NONNULL_BEGIN

typedef void (^GatewayTLSPinningChallengeHandler)(NSURLSession *session,
                                                  NSURLAuthenticationChallenge *challenge,
                                                  void (^completionHandler)(NSURLSessionAuthChallengeDisposition disposition,
                                                                            NSURLCredential * _Nullable credential));

@interface GatewayTLSPinningSessionDelegate : NSObject <NSURLSessionDelegate>
@property (nonatomic, copy, nullable) GatewayTLSPinningChallengeHandler challengeHandler;
@end

NS_ASSUME_NONNULL_END
